# OronBox Resource Publishing Agreement

Version: `1.0.0`  
Effective date: `2026-07-19`

## 1. Scope

This Agreement applies to users who upload, submit for review, publish, update, or distribute resources to third-party platforms through the official OronBox resource service

## 2. Publisher warranties

The publisher shall use a BandBBS account under their lawful control and shall obtain sufficient authorization before acting for a team, organization, employer, client, or co-author

The publisher warrants that they hold all rights or permissions necessary for submitted programs, watchfaces, images, text, fonts, audio, trademarks, and other materials. Malicious code, unlawful content, device-damaging content, and content infringing intellectual property or other lawful rights are prohibited

The selected platform, resource type, supported devices, description, version, features, pricing, update notes, and external publication information must be accurate. Known compatibility or security problems must not be concealed. VelaOS and Zepp OS are distinct platforms and similarly named files are not presumed compatible

OronBox may request licenses, authorization records, source attribution, original projects, submission history, or other reasonable proof when resolving safety, complaint, or ownership concerns

## 3. License to OronBox

### 3.1 Grant

For the duration that the resource service hosts the resource, the publisher grants OronBox a worldwide, royalty-free, non-exclusive license to:

- store, copy, and distribute the resource for download
- identify its platform, type, package ID, and version
- proportionally resize images and convert them to WebP
- display its name, description, author name, and images
- replicate approved public blobs to the Cloudflare R2 Asia-Pacific region
- submit content to BandBBS or AstroBox when explicitly selected by the publisher

This license includes necessary unpacking, validation, payload extraction, content-addressed deduplication, security checks, format migration, backup recovery, and publication metadata generation

### 3.2 Service providers

OronBox may sublicense only the rights necessary for infrastructure providers to operate servers, object storage, and network distribution. Unpublished resources may not be used for unrelated purposes

### 3.3 Reserved rights

The publisher retains all rights not expressly granted. This Agreement does not transfer ownership or copyright

After delisting, OronBox will stop new public distribution, but reasonable backups, review records, shared blobs, and copies already retained by third parties may not disappear immediately

## 4. Uploads and versions

The publisher must select a platform and resource type before uploading at least one preview image and one resource file, and must bind each file to supported devices

OronBox analyzes file contents. A submission may be rejected when the detected platform or type differs from the publisher's selection

Submission creates an immutable revision snapshot containing text, images, files, analysis results, device bindings, and external publication settings. Later changes require a new revision and a new review

A resource cannot be changed from VelaOS to Zepp OS, or vice versa. One file may support multiple devices and one revision may contain multiple device-specific files

Images are proportionally resized so neither dimension exceeds 1500 px, then converted to WebP. AstroBox icons must be supplied as square images and covers as 3:2 images; OronBox does not crop or correct their aspect ratio

## 5. OronBox review

Resources must pass the OronBox Resource Review Rules before publication. Those Rules form part of this Agreement and describe review standards, rejection grounds, enforcement, and appeals

Review may cover completeness, platform and type, device bindings, description, security and privacy, rights and provenance, quality, and external platform requirements. Approval is not a complete code audit, a guarantee, or an endorsement

Approved revisions enter the OronBox resource catalog and trigger selected external publication jobs. OronBox may suspend downloads, reject updates, delist resources, or contact third-party platforms when it discovers safety, infringement, or legal concerns

## 6. Publishing to BandBBS

BandBBS publication requires a separate publishing grant belonging to the same BandBBS user as the active OronBox account. Ordinary sign-in permission cannot publish resources

Within the confirmed resource scope, OronBox may upload files and images, set required metadata, create a BandBBS resource, update a BandBBS resource previously linked to the same OronBox resource, and record its result, ID, and URL

The grant is not exposed to plugins or unrelated third parties. BandBBS publication has no additional OronBox-defined review step, but BandBBS may independently moderate or remove content. Revoking the grant stops future operations and does not delete existing publications

## 7. Publishing to AstroBox

Only VelaOS quick apps and watchfaces may target AstroBox. Zepp OS resources cannot be submitted to AstroBox-Repo

With the user's GitHub identity, OronBox may create or update a resource repository, fork AstroBox-Repo, create branches and commits, and open a pull request. These records are normally public and may display the user's GitHub name

In delegated mode, the same operations use the server-held `oronbox-community` identity. Delegation is a technical instruction for one confirmed revision; it does not transfer ownership, authorship, or responsibility

OronBox may generate files, manifests, catalog entries, repositories, branches, commits, forks, and pull requests, and query their review state. Mechanical changes to paths, file names, compression, manifest formatting, and device identifiers may be automated. Material changes to authorship, name, pricing, license, supported devices, description, or resource files require renewed confirmation

AstroBox-Repo maintainers review pull requests independently. OronBox neither guarantees a merge nor represents endorsement by AstroBox. Revoking GitHub authorization does not erase public repository history

## 8. Confirmation for each publication

Before each revision is submitted, OronBox shall display the version, files, images, supported devices, targets, and acting identity. Confirmation authorizes only the operations shown for that revision

The confirmation shall identify the resource name, version, platform and type; file and image bindings; OronBox visibility; BandBBS category and create/update action; AstroBox Item ID, tags and pricing; GitHub or delegated identity; and repositories or pull requests to be created or updated

New OAuth scopes, a wider operation scope, or a changed publication identity require a new explanation and confirmation

## 9. Complaints and delisting

Rights holders and users may report security, infringement, or unlawful content to `t3164473115@163.com`, identifying the resource, issue, supporting basis, and contact details

OronBox may request evidence, consult the publisher, preserve necessary records, and impose temporary or permanent restrictions including review suspension, download suspension, update rejection, material removal, version delisting, resource delisting, or publishing restrictions. Imminent malware, credential theft, device danger, serious infringement, or rapidly expanding harm may be restricted before full verification

OronBox actions do not automatically change content hosted by BandBBS, GitHub, or AstroBox. Requests concerning those platforms may also need to be filed through their own channels. Knowingly false complaints, impersonation, harassment, or abuse of the complaint system may be rejected and sanctioned under the Review Rules

## 10. Deletion, revocation, and history

Deleting a draft or delisting a OronBox resource does not automatically remove BandBBS, GitHub, or AstroBox publications. Commits, pull requests, forks, comments, reviews, sanctions, downloads, caches, and reposted copies may remain

Content-addressed blobs are not physically deleted while referenced by another resource, revision, necessary complaint evidence, or backup. Revoking OAuth or delegated publication only prevents future operations

## 11. External publication failures

External publication may fail because of expired authorization, insufficient permissions, API changes, account restrictions, network failures, invalid formats, or third-party review. OronBox may retry operations that do not materially change the confirmed publication, but stops when authorization is revoked, the job is cancelled, or risk controls require termination

OronBox does not guarantee continued BandBBS availability or acceptance of an AstroBox pull request

## 12. Risks and disclaimer

OronBox approval is not an express or implied guarantee of safety, compatibility, clear title, or fitness. Users remain responsible for checking their device model, operating-system version, and resource source before installation under the OronBox Terms and Disclaimer

## 13. Changes and termination

Changes that expand the necessary license, external targets, OAuth scopes, or delegated identity require an updated agreement and renewed consent where necessary. Refusing new third-party terms only disables that target where functions can remain separate

Termination does not affect publications, public records, rights, duties, or disputes arising before termination

## 14. Publisher responsibility

If a publisher intentionally or negligently breaches their warranties or this Agreement and causes OronBox or its maintainers direct losses established by a final judgment or other lawful determination, the publisher bears responsibility to the extent permitted by applicable law. OronBox shall take reasonable steps to mitigate loss

## 15. Governing law and disputes

The governing-law and dispute-resolution provisions of the OronBox Terms and Disclaimer apply to this Agreement

## 16. Contact

- Official instance operator: OronBox Operations Team
- Publishing and complaints: `t3164473115@163.com`
- Public discussion: <https://github.com/zxor-org/OronBox/discussions>
