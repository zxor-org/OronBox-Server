package schema

import _ "embed"

//go:embed sql/community_notifications_v8.sql
var communityNotificationsV8 string

//go:embed sql/community_schema_v8.sql
var communitySchemaV8 string

//go:embed sql/governance_schema.sql
var governanceSchema string

//go:embed sql/notification_functions.sql
var notificationFunctions string

//go:embed sql/notification_triggers_v6.sql
var notificationTriggersV6 string

//go:embed sql/reason_template_seed.sql
var reasonTemplateSeed string

//go:embed sql/fresh.sql
var schema string

//go:embed sql/binding_identities_v9.sql
var bindingIdentitiesV9 string

//go:embed sql/resource_content_v9.sql
var resourceContentV9 string

//go:embed sql/deletion_v10.sql
var deletionV10 string

//go:embed sql/collections_v11.sql
var collectionsV11 string

//go:embed sql/releases_v12.sql
var releasesV12 string

//go:embed sql/blog_v13.sql
var blogV13 string

//go:embed sql/binding_claims_v14.sql
var bindingClaimsV14 string

//go:embed sql/credentials_v15.sql
var credentialsV15 string
