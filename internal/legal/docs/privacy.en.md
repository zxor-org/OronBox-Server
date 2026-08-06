# OronBox Privacy Notice

Version: 1.0.0
Effective date: 2026-07-19

## 1. Scope

This notice applies to the official OronBox online service. It does not apply to self-hosted OronBox Server instances, and it does not replace the privacy notices of BandBBS, GitHub, Cloudflare, or other third parties.

## 2. Information processed

BandBBS OAuth login processes the BandBBS user ID, username, avatar, authorized scopes, and credentials required to complete login.

BandBBS publishing uses a separate grant and additionally processes the authorization credential, target category, resource ID, attachments, and publication status. A normal login grant is not a publication grant.

Publishing AstroBox resources through your GitHub account processes the GitHub user ID, username, authorized scopes, access token, and the temporary verification data required to complete authorization.

The service also processes OronBox sessions, client platform and version, IP address, user agent, and operational records needed for login, review, and service security.

When you create a resource, the service processes the name, description, images, resource files, device bindings, review records, and external publication configuration you submit.

When handling rights complaints, security incidents, or appeals, the service may process complaint content, rights evidence, creator responses, file hashes, review opinions, and directly relevant evidence.

Except where required by law or a specific dispute, OronBox does not request identity numbers, precise addresses, biometric, financial, health, or other information unrelated to the service.

## 3. Purposes

This information is used only to:

- Authenticate users and maintain OronBox sessions
- Store, review, display, and distribute resources
- Publish to BandBBS or AstroBox at your choice
- Sync download copies of public resources
- Prevent abuse, diagnose faults, and handle rights complaints

OronBox does not use this information for advertising, marketing profiles, or sale to third parties.

## 4. OAuth and security

The read-only BandBBS access token is stored on your device, and the app uses it to call BandBBS directly. Other OAuth tokens are encrypted on the server and never handed to the client. OronBox session tokens are stored only as non-reversible hashes.

OAuth tokens, authorization headers, session tokens, and similar sensitive information are not written to ordinary logs.

You can revoke OAuth grants or sign out of OronBox. Revocation stops future operations but does not automatically delete content already published to third parties or public history.

BandBBS login, BandBBS publication, and GitHub publication are separate grants. Normal BandBBS login does not authorize creating or modifying BandBBS resources, and a GitHub grant does not mean you have confirmed a specific repository or pull request operation.

## 5. Storage location

The primary server is located in Hong Kong, China. Accounts, OAuth credentials, sessions, drafts, resources, review records, and file objects are processed there.

Approved public files and images are copied to Cloudflare R2 for public download distribution. R2 does not hold OAuth tokens, account profiles, unpublished drafts, or internal audit logs.

Data you submit through the official online service is processed in Hong Kong. Public resource copies may be stored and distributed through Cloudflare's network.

## 6. Disclosure and publication

Once a resource is published, its name, description, author name, images, files, supported devices, and version information become public.

When you choose BandBBS publication, OronBox sends the content and identity information needed to create or update the resource to BandBBS.

When you choose AstroBox publication, OronBox submits the resource repository content, author name, commit, and pull request to GitHub and AstroBox-Repo.

Public resources may be downloaded, mirrored, or cached by others. OronBox cannot erase every external copy.

For infringement, security, or platform-abuse incidents, OronBox may provide the minimum necessary resources, external identifiers, file hashes, publication times, and review records to relevant platforms, rightsholders, or authorities.

IP, user-agent, device, and session information is provided only when reasonably necessary for serious account-security, automated-abuse, ban-evasion, or other serious investigations.

Storage and network distribution of public resource copies is provided by Cloudflare, Inc.

## 7. Retention and deletion

OAuth temporary verification data expires within minutes. OronBox session access tokens last 15 minutes and refresh tokens last 30 days; rotation, revocation, or account status changes may invalidate them earlier.

BandBBS and GitHub OAuth credentials are kept until you revoke the grant, unbind the account, or they are no longer needed.

Unsubmitted drafts are kept for 180 days after the last edit. Drafts you delete are removed from the active database immediately.

Ordinary service logs are kept for 30 days, security audit records for 180 days, and complaint, report, and appeal materials for one year after the case closes.

Public resources, versions, images, and public metadata are kept while the resource is public. Necessary review records for delisted resources are kept for one year after delisting.

After you revoke or unbind an OAuth grant, the credential is removed from the active database immediately. After account deletion, account data is deleted or anonymized within seven days.

Deletion may be suspended for an unresolved complaint, security incident, or legal duty, but only the minimum necessary information is retained.

## 8. User requests

You may request access to or correction of your account data, revocation of grants, deletion of unpublished drafts, or deletion of personal information no longer needed.

Content already published to BandBBS, GitHub, or AstroBox must be handled through the corresponding platform.

After account deletion, active sessions are invalidated and future OAuth operations stop, but the minimum records necessary for security, complaints, or disputes may be kept.

Submit requests through in-app feedback or email. After necessary identity verification, OronBox will respond within 15 working days and explain any necessary extension.

## 9. Self-hosting

A self-hosted operator independently controls the data on its server and must disclose its identity, storage location, logs, OAuth configuration, and data processing.

OronBox project maintainers cannot access or control data on self-hosted instances.

## 10. Security

The official service uses measures proportional to its scale and risk, including HTTPS, short-lived login tickets, session rotation, token encryption, least privilege, access control, and log redaction.

In the event of a security incident that may affect users, maintainers will control the risk, revoke affected credentials, preserve necessary evidence, and notify affected users or authorities where reasonably possible and required by law.

No internet service is absolutely secure. You should also protect your device, accounts, and authorization credentials.

## 11. Minors

OronBox is intended for wearable-device users with the corresponding understanding and ability. Minors should use account authorization, resource publication, and device-affecting features under guardian guidance.

## 12. Changes and contact

New purposes, broader OAuth scopes or third-party disclosures, or a changed primary storage location require an updated notice and renewed confirmation where necessary.

Governing law and dispute resolution related to the official online service follow the Terms of Use and Disclaimer.

- Operator: OronBox Operations Team
- Privacy and data requests: t3164473115@163.com
- Discussions: <https://github.com/zxor-org/OronBox/discussions>
