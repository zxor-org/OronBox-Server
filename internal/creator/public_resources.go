package creator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Pick the highest package version exposed by the revision.
const highestVersionSQL = `COALESCE((SELECT package_version FROM revision_artifacts WHERE revision_id=rr.id AND package_version<>'' ORDER BY length(package_version) DESC,package_version DESC LIMIT 1),'')`

const collectionPublishedAtSQL = `GREATEST(
	COALESCE(c.published_at,cr.created_at),
	COALESCE((SELECT max(COALESCE(child.published_at,child_revision.created_at))
		FROM resources child
		JOIN resource_revisions child_revision ON child_revision.id=child.current_revision_id
		WHERE child.collection_id=c.id AND child.moderation_state='visible'),COALESCE(c.published_at,cr.created_at))
)`

// Recommendation jitter is deterministic for a resource and request seed so
// pagination remains stable while a fresh seed reshuffles the catalog.
const publicCardsSQLTemplate = `WITH recent_resource_coins AS (
SELECT ledger.reference_id resource_id,
count(DISTINCT ledger.user_id)::numeric unique_coiners,
COALESCE(sum(-ledger.delta_units / 10),0)::numeric coins
FROM coin_ledger ledger
JOIN resource_coin_votes vote ON vote.resource_id::text=ledger.reference_id AND vote.user_id=ledger.user_id AND vote.invalidated_at IS NULL
	WHERE NOT $6 AND ledger.kind='resource_vote' AND ledger.reference_type='resource' AND ledger.created_at >= now() - interval '14 days'
GROUP BY ledger.reference_id
), cards AS (
SELECT 'resource'::text card_type,r.id::text id,r.slug,rr.name,rr.summary,u.username owner,u.bandbbs_user_id,u.avatar_url,r.kind,rr.paid_type,
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='preview' ORDER BY position LIMIT 1),'') preview,
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='icon' ORDER BY position LIMIT 1),'') icon,
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='cover' ORDER BY position LIMIT 1),'') cover,
` + highestVersionSQL + ` version,
COALESCE((SELECT jsonb_agg(DISTINCT d.codename) FROM revision_artifacts a JOIN revision_artifact_devices b ON b.artifact_id=a.id JOIN devices d ON d.id=b.device_id WHERE a.revision_id=rr.id),'[]') devices,
COALESCE((SELECT jsonb_agg(attribute ORDER BY attribute) FROM resource_revision_attributes WHERE revision_id=rr.id),'[]') attributes,
r.download_count,r.curation_grade,COALESCE((SELECT sum(v.coins) FROM resource_coin_votes v WHERE v.resource_id=r.id AND v.invalidated_at IS NULL),0)::bigint coins,
	COALESCE(r.collection_id::text,'') collection_id,COALESCE((SELECT revision.name FROM resource_collections collection JOIN resource_collection_revisions revision ON revision.id=collection.current_revision_id WHERE collection.id=r.collection_id),'') collection_name,
	0 resource_count,COALESCE(r.published_at,rr.created_at) published_at,r.updated_at,
	CASE WHEN $6 THEN 0::numeric ELSE RESOURCE_SCORE *
		 RESOURCE_JITTER END recommendation_score
FROM resources r JOIN resource_revisions rr ON rr.id=r.current_revision_id JOIN users u ON u.id=r.owner_id
LEFT JOIN recent_resource_coins recent ON recent.resource_id=r.id::text
WHERE r.moderation_state='visible' AND ($1='' OR rr.name ILIKE '%'||$1||'%' OR rr.summary ILIKE '%'||$1||'%' OR u.username ILIKE '%'||$1||'%') AND ($2='' OR r.kind=$2)
AND (cardinality($3::text[])=0 OR EXISTS(SELECT 1 FROM revision_artifacts a JOIN revision_artifact_devices b ON b.artifact_id=a.id JOIN devices d ON d.id=b.device_id WHERE a.revision_id=rr.id AND d.codename=ANY($3::text[])))
AND (cardinality($4::text[])=0 OR EXISTS(SELECT 1 FROM resource_revision_attributes attribute WHERE attribute.revision_id=rr.id AND attribute.attribute=ANY($4::text[])))
AND ($1<>'' OR r.collection_id IS NULL OR NOT EXISTS(SELECT 1 FROM resource_collections c WHERE c.id=r.collection_id AND c.current_revision_id IS NOT NULL AND c.enabled))
UNION ALL
SELECT 'collection',c.id::text,c.slug,cr.name,cr.summary,u.username,u.bandbbs_user_id,u.avatar_url,c.kind,'free'::text,
COALESCE((SELECT m.blob_sha256 FROM resources representative JOIN revision_media m ON m.revision_id=representative.current_revision_id AND m.role='preview' WHERE representative.id=c.representative_resource_id AND representative.moderation_state='visible' ORDER BY m.position LIMIT 1),''),
COALESCE(cr.icon_sha256,(SELECT m.blob_sha256 FROM resources representative JOIN revision_media m ON m.revision_id=representative.current_revision_id AND m.role='icon' WHERE representative.id=c.representative_resource_id AND representative.moderation_state='visible' ORDER BY m.position LIMIT 1),''),
COALESCE(cr.cover_sha256,(SELECT m.blob_sha256 FROM resources representative JOIN revision_media m ON m.revision_id=representative.current_revision_id AND m.role='cover' WHERE representative.id=c.representative_resource_id AND representative.moderation_state='visible' ORDER BY m.position LIMIT 1),''),
COALESCE((SELECT a.package_version FROM resources representative JOIN revision_artifacts a ON a.revision_id=representative.current_revision_id WHERE representative.id=c.representative_resource_id AND a.package_version<>'' ORDER BY length(a.package_version) DESC,a.package_version DESC LIMIT 1),''),
COALESCE((SELECT jsonb_agg(DISTINCT d.codename) FROM resources child JOIN revision_artifacts a ON a.revision_id=child.current_revision_id JOIN revision_artifact_devices b ON b.artifact_id=a.id JOIN devices d ON d.id=b.device_id WHERE child.collection_id=c.id AND child.moderation_state='visible'),'[]'),
COALESCE((SELECT jsonb_agg(DISTINCT attribute.attribute) FROM resources child JOIN resource_revision_attributes attribute ON attribute.revision_id=child.current_revision_id WHERE child.collection_id=c.id AND child.moderation_state='visible'),'[]'),
COALESCE((SELECT sum(child.download_count) FROM resources child WHERE child.collection_id=c.id AND child.moderation_state='visible'),0)::integer,
CASE WHEN EXISTS(SELECT 1 FROM resources child WHERE child.collection_id=c.id AND child.moderation_state='visible' AND child.curation_grade='featured') THEN 'featured' ELSE 'standard' END,
COALESCE((SELECT sum(v.coins) FROM resources child JOIN resource_coin_votes v ON v.resource_id=child.id AND v.invalidated_at IS NULL WHERE child.collection_id=c.id AND child.moderation_state='visible'),0)::bigint,
	c.id::text,cr.name,(SELECT count(*) FROM resources child WHERE child.collection_id=c.id AND child.moderation_state='visible' AND child.current_revision_id IS NOT NULL)::integer,` + collectionPublishedAtSQL + ` published_at,c.updated_at,
		CASE WHEN $6 THEN 0::numeric ELSE COLLECTION_SCORE *
		COLLECTION_JITTER END recommendation_score
FROM resource_collections c JOIN resource_collection_revisions cr ON cr.id=c.current_revision_id JOIN users u ON u.id=c.owner_id
WHERE c.enabled AND ($1='' OR cr.name ILIKE '%'||$1||'%' OR cr.summary ILIKE '%'||$1||'%' OR u.username ILIKE '%'||$1||'%') AND ($2='' OR c.kind=$2)
AND EXISTS(SELECT 1 FROM resources child WHERE child.collection_id=c.id AND child.moderation_state='visible' AND child.current_revision_id IS NOT NULL)
AND (cardinality($3::text[])=0 OR EXISTS(SELECT 1 FROM resources child JOIN revision_artifacts a ON a.revision_id=child.current_revision_id JOIN revision_artifact_devices b ON b.artifact_id=a.id JOIN devices d ON d.id=b.device_id WHERE child.collection_id=c.id AND child.moderation_state='visible' AND d.codename=ANY($3::text[])))
AND (cardinality($4::text[])=0 OR EXISTS(SELECT 1 FROM resources child JOIN resource_revision_attributes attribute ON attribute.revision_id=child.current_revision_id WHERE child.collection_id=c.id AND child.moderation_state='visible' AND attribute.attribute=ANY($4::text[])))
) `

// publicCardsSQL renders the cards query with the ranking multipliers baked
// into the score expressions. The values are normalized first so a bad
// configuration can never inject a non-finite literal.
func publicCardsSQL(ranking Ranking) string {
	ranking = ranking.normalized()
	resourceScore := fmt.Sprintf(`((1.0 + ln(1.0 + COALESCE(recent.unique_coiners,0) + %g * GREATEST(COALESCE(recent.coins,0)-COALESCE(recent.unique_coiners,0),0)) + %g * ln(1.0 + GREATEST(r.download_count,0))) *
	 (1.0 + %g * exp(-GREATEST(EXTRACT(EPOCH FROM (now()-COALESCE(r.published_at,rr.created_at)))/86400.0,0)/%g)) *
	 CASE WHEN r.curation_grade='featured' THEN %g ELSE 1.0 END *
	 COALESCE((SELECT exp(sum(ln(definition.coefficient))) FROM resource_revision_attributes binding JOIN resource_attributes definition ON definition.id=binding.attribute WHERE binding.revision_id=rr.id),1.0))`,
		ranking.CoinExtraWeight, ranking.DownloadWeight, ranking.FreshnessAmplitude, ranking.FreshnessDecayDays, ranking.FeaturedBoost)
	collectionScore := fmt.Sprintf(`((1.0 + ln(1.0 + COALESCE((SELECT sum(recent.unique_coiners) FROM resources child JOIN recent_resource_coins recent ON recent.resource_id=child.id::text WHERE child.collection_id=c.id AND child.moderation_state='visible'),0) +
	 %g * COALESCE((SELECT sum(GREATEST(recent.coins-recent.unique_coiners,0)) FROM resources child JOIN recent_resource_coins recent ON recent.resource_id=child.id::text WHERE child.collection_id=c.id AND child.moderation_state='visible'),0)) +
	 %g * ln(1.0 + COALESCE((SELECT sum(child.download_count) FROM resources child WHERE child.collection_id=c.id AND child.moderation_state='visible'),0))) *
	 (1.0 + %g * exp(-GREATEST(EXTRACT(EPOCH FROM (now()-(`+collectionPublishedAtSQL+`)))/86400.0,0)/%g)) *
	 CASE WHEN EXISTS(SELECT 1 FROM resources child WHERE child.collection_id=c.id AND child.moderation_state='visible' AND child.curation_grade='featured') THEN %g ELSE 1.0 END *
	 COALESCE((SELECT exp(sum(ln(definition.coefficient))) FROM resource_attributes definition WHERE definition.id IN (SELECT DISTINCT binding.attribute FROM resources child JOIN resource_revision_attributes binding ON binding.revision_id=child.current_revision_id WHERE child.collection_id=c.id AND child.moderation_state='visible')),1.0))`,
		ranking.CoinExtraWeight, ranking.DownloadWeight, ranking.FreshnessAmplitude, ranking.FreshnessDecayDays, ranking.FeaturedBoost)
	resourceJitter := fmt.Sprintf("(%g + (((hashtextextended(r.id::text,$5) %% 10000 + 10000) %% 10000)::numeric / 10000.0))", ranking.JitterBase)
	collectionJitter := fmt.Sprintf("(%g + (((hashtextextended(c.id::text,$5) %% 10000 + 10000) %% 10000)::numeric / 10000.0))", ranking.JitterBase)
	sql := strings.ReplaceAll(publicCardsSQLTemplate, "RESOURCE_SCORE", resourceScore)
	sql = strings.ReplaceAll(sql, "COLLECTION_SCORE", collectionScore)
	sql = strings.ReplaceAll(sql, "RESOURCE_JITTER", resourceJitter)
	return strings.ReplaceAll(sql, "COLLECTION_JITTER", collectionJitter)
}

func normalizePublicQuery(query PublicQuery) PublicQuery {
	if query.Limit <= 0 || query.Limit > 100 {
		query.Limit = 50
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	if query.Devices == nil {
		query.Devices = []string{}
	}
	if query.Attributes == nil {
		query.Attributes = []string{}
	}
	seed := query.Seed
	if seed == 0 {
		seed = time.Now().UTC().Unix() / 86400
	}
	query.Seed = seed
	return query
}

func publicResourceOrder(sortBy string) string {
	order := "recommendation_score DESC, published_at DESC, id DESC"
	if sortBy == "name" {
		order = "name ASC"
	} else if sortBy == "random" {
		order = "random()"
	} else if sortBy == "time" {
		order = "published_at DESC, id DESC"
	}
	return order
}

func (s *Service) publicResourceRows(ctx context.Context, query PublicQuery) ([]PublicResource, error) {
	query = normalizePublicQuery(query)
	rows, err := s.db.QueryContext(ctx, publicCardsSQL(s.RankingSettings(ctx))+`SELECT card_type,id,slug,name,summary,owner,bandbbs_user_id,avatar_url,kind,paid_type,preview,icon,cover,version,devices,attributes,download_count,curation_grade,coins,collection_id,collection_name,resource_count,published_at,updated_at FROM cards WHERE ($7=false OR paid_type='free') AND ($8=false OR paid_type<>'force_paid') AND ($9=false OR curation_grade='featured') ORDER BY `+publicResourceOrder(query.Sort)+` LIMIT $10 OFFSET $11`, query.Search, query.Kind, query.Devices, query.Attributes, query.Seed, query.SkipRecommendationScore, query.HidePaid, query.HideForcePaid, query.Featured, query.Limit, query.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PublicResource, 0)
	for rows.Next() {
		var item PublicResource
		var devices, attributes []byte
		if err := rows.Scan(&item.CardType, &item.ID, &item.Slug, &item.Name, &item.Summary, &item.Owner, &item.OwnerBandBBSUserID, &item.OwnerAvatarURL, &item.Kind, &item.PaidType, &item.PreviewSHA256, &item.IconSHA256, &item.CoverSHA256, &item.Version, &devices, &attributes, &item.DownloadCount, &item.CurationGrade, &item.CoinCount, &item.CollectionID, &item.CollectionName, &item.ResourceCount, &item.PublishedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(devices, &item.Devices)
		_ = json.Unmarshal(attributes, &item.Attributes)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) PublicResources(ctx context.Context, query PublicQuery) ([]PublicResource, int, error) {
	query = normalizePublicQuery(query)
	var total int
	if err := s.db.QueryRowContext(ctx, publicCardsSQL(s.RankingSettings(ctx))+`SELECT count(*) FROM cards WHERE ($7=false OR paid_type='free') AND ($8=false OR paid_type<>'force_paid') AND ($9=false OR curation_grade='featured')`, query.Search, query.Kind, query.Devices, query.Attributes, query.Seed, query.SkipRecommendationScore, query.HidePaid, query.HideForcePaid, query.Featured).Scan(&total); err != nil {
		return nil, 0, err
	}
	result, err := s.publicResourceRows(ctx, query)
	return result, total, err
}

func (s *Service) PublicResource(ctx context.Context, resourceID string) (PublicResourceDetail, error) {
	var description, descriptionFormat string
	var summary PublicResource
	var devices []byte
	var revisionID string
	var purchaseLink, purchaseCurrency string
	var purchasePrice sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT r.id::text,r.slug,rr.name,rr.summary,rr.description,rr.description_format,u.username,u.bandbbs_user_id,u.avatar_url,r.kind,rr.paid_type,
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='preview' ORDER BY position LIMIT 1),''),
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='icon' ORDER BY position LIMIT 1),''),
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='cover' ORDER BY position LIMIT 1),''),
`+highestVersionSQL+`,
COALESCE((SELECT jsonb_agg(DISTINCT d.codename) FROM revision_artifacts a JOIN revision_artifact_devices b ON b.artifact_id=a.id JOIN devices d ON d.id=b.device_id WHERE a.revision_id=rr.id),'[]'),r.download_count,r.curation_grade,
COALESCE((SELECT sum(v.coins) FROM resource_coin_votes v WHERE v.resource_id=r.id AND v.invalidated_at IS NULL),0),COALESCE(r.collection_id::text,''),
COALESCE((SELECT revision.name FROM resource_collections collection JOIN resource_collection_revisions revision ON revision.id=collection.current_revision_id WHERE collection.id=r.collection_id),''),COALESCE(r.published_at,rr.created_at),r.updated_at,rr.id::text
,
COALESCE(rr.purchase_link,''),rr.purchase_price,CASE WHEN COALESCE(rr.purchase_link,'') <> '' THEN 'CNY' ELSE '' END
FROM resources r JOIN resource_revisions rr ON rr.id=r.current_revision_id JOIN users u ON u.id=r.owner_id WHERE r.id=$1 AND r.moderation_state='visible'`, resourceID).
		Scan(&summary.ID, &summary.Slug, &summary.Name, &summary.Summary, &description, &descriptionFormat, &summary.Owner, &summary.OwnerBandBBSUserID, &summary.OwnerAvatarURL, &summary.Kind, &summary.PaidType, &summary.PreviewSHA256, &summary.IconSHA256, &summary.CoverSHA256, &summary.Version, &devices, &summary.DownloadCount, &summary.CurationGrade, &summary.CoinCount, &summary.CollectionID, &summary.CollectionName, &summary.PublishedAt, &summary.UpdatedAt, &revisionID, &purchaseLink, &purchasePrice, &purchaseCurrency)
	if errors.Is(err, sql.ErrNoRows) {
		return PublicResourceDetail{}, ErrNotFound
	}
	if err != nil {
		return PublicResourceDetail{}, err
	}
	_ = json.Unmarshal(devices, &summary.Devices)
	attributeRows, err := s.db.QueryContext(ctx, `SELECT attribute FROM resource_revision_attributes WHERE revision_id=$1 ORDER BY attribute`, revisionID)
	if err != nil {
		return PublicResourceDetail{}, err
	}
	for attributeRows.Next() {
		var attribute string
		if err := attributeRows.Scan(&attribute); err != nil {
			attributeRows.Close()
			return PublicResourceDetail{}, err
		}
		summary.Attributes = append(summary.Attributes, attribute)
	}
	if err := attributeRows.Err(); err != nil {
		attributeRows.Close()
		return PublicResourceDetail{}, err
	}
	attributeRows.Close()
	summary.CardType = "resource"
	media, err := s.revisionMedia(ctx, revisionID)
	if err != nil {
		return PublicResourceDetail{}, err
	}
	artifacts, err := s.revisionArtifacts(ctx, revisionID, true)
	if err != nil {
		return PublicResourceDetail{}, err
	}
	links, err := s.revisionLinks(ctx, revisionID)
	if err != nil {
		return PublicResourceDetail{}, err
	}
	collaborators, source, err := s.ResourceRelationships(ctx, resourceID)
	if err != nil {
		return PublicResourceDetail{}, err
	}
	accepted := collaborators[:0]
	for _, collaborator := range collaborators {
		if collaborator.AcceptedAt != nil {
			accepted = append(accepted, collaborator)
		}
	}
	var publicPrice *float64
	if purchasePrice.Valid {
		value := purchasePrice.Float64
		publicPrice = &value
	}
	return PublicResourceDetail{Description: description, DescriptionFormat: descriptionFormat, PublicResource: summary, Media: media, Artifacts: artifacts, Collaborators: accepted, Source: source, Links: links, PurchaseLink: purchaseLink, PurchasePrice: publicPrice, PurchaseCurrency: purchaseCurrency}, nil
}
