# OronBox Privacy Notice

Version: `1.0.0`  
Effective date: `2026-07-19`

## 1. Scope

This notice applies to the official OronBox online service. It does not apply to self-hosted OronBox Server instances or replace the privacy notices of BandBBS, GitHub, Cloudflare, or other third parties.

## 2. Information processed

BandBBS OAuth login processes the BandBBS user ID, username, avatar, scopes, and credentials required to complete login. BandBBS publishing uses a separate grant and additionally processes the target category, resource ID, attachments, and publication status.

Publishing through the user's GitHub account processes the GitHub user ID, username, scopes, access token, and the one-time state and temporary PKCE verification data required for Web OAuth. Device Flow used by the CLI or a headless environment additionally processes the user code and its temporary authorization state.

The service also processes OronBox sessions, client platform and version, IP address, user agent, submitted resource metadata, images, files, device bindings, review records, publication configuration, feedback, complaints, and directly relevant evidence.

OronBox does not intentionally request identity numbers, precise addresses, biometric, financial, health, or other information unrelated to the current service.

Resource files may be deduplicated by a content-derived unique value. This content-addressed storage changes only physical storage and does not expose private files to other users.

## 3. Purposes

Information is used only to authenticate users, maintain sessions, store and review resources, perform user-selected publication, distribute public resources, prevent abuse, diagnose faults, and resolve feedback or rights complaints. OAuth data is not sold or used for advertising profiles.

## 4. OAuth and security

The read-only BandBBS access token is stored on the user's device, and the app uses it to call the BandBBS API directly. BandBBS refresh tokens, the BandBBS publication grant, and GitHub OAuth tokens always remain encrypted in PostgreSQL and are never handed to the client. OronBox session tokens are stored only as non-reversible hashes. Tokens, authorization headers and codes, and complete OAuth request bodies must not be written to ordinary logs.

BandBBS login, BandBBS publication, and GitHub publication are separate grants. Revoking a grant prevents future operations but does not erase content already published to a third party.

## 5. Storage location

The primary server is in Hong Kong, China and is provided by Guangzhou Runyu Technology Co., Ltd. Account, OAuth, session, draft, resource, review, and authoritative file data are processed there.

Approved public files and images may be copied to Cloudflare R2 in the Asia-Pacific region. R2 is a public replica, not authoritative storage, and does not hold OAuth tokens, private drafts, account profiles, or internal audit logs. An Asia-Pacific location hint does not mean data remains exclusively in Hong Kong.

Blob means one stored file object. SHA-256 is a fixed-length value derived from file content. A blob is physically removed only after no active resource, version, evidence record, or backup references it.

## 6. Disclosure and publication

Published resource names, descriptions, author names, images, files, supported devices, and versions become public. User-selected BandBBS or AstroBox publication sends the necessary content and identity information to the corresponding platform.

Public resources may be downloaded, mirrored, or cached by others. OronBox cannot erase every external copy.

For infringement, security, or abuse incidents, OronBox may provide the minimum necessary resource, identifier, hash, publication time, and review record. Tokens and unrelated drafts or histories are not routinely disclosed.

Cloudflare, Inc. provides public replica storage and network distribution. Guangzhou Runyu Technology Co., Ltd. provides the Hong Kong server.

## 7. Retention and deletion

- OronBox login ticket: up to 3 minutes
- OAuth state and temporary PKCE verification data: up to 10 minutes
- GitHub Device Flow data: until the expiry returned by GitHub
- Access token: 15 minutes
- Refresh token: 30 days, unless rotated or revoked earlier
- Unsubmitted draft: 180 days after last edit
- Ordinary server logs: 30 days
- Security and audit records: 180 days
- Closed feedback, complaints, and appeals: one year after closure
- Necessary review records for removed resources: one year
- Unreferenced blobs: seven-day grace period; backup copies up to 30 days
- Revoked OAuth credentials: immediate removal from the active database; encrypted backups up to 30 days
- Deleted account data: deletion or anonymization from the active database within seven days; backups up to 30 days

Public resource data remains while the resource is public. Backup data is not restored for ordinary operations; after disaster recovery, deletion and revocation processing must be re-applied.

Deletion may be suspended for an unresolved complaint, security incident, or legal duty, but only the minimum necessary information may be retained.

## 8. User requests

Users may request access or correction, revoke grants, delete private drafts, or request deletion of information no longer needed. External BandBBS, GitHub, or AstroBox content must also be handled through that platform.

Requests may be submitted through in-app feedback or email. After necessary identity verification, OronBox will respond within 15 working days and explain any necessary extension.

## 9. Self-hosting

A self-hosted operator independently controls its server data and must disclose its identity, storage, logs, OAuth configuration, and processing. OronBox project maintainers cannot access or control that instance's data.

## 10. Security

The official service should use HTTPS, OAuth state, short-lived tickets, session rotation, token encryption, least privilege, access controls, backups, and log redaction appropriate to its scale and risks. No internet service is absolutely secure.

## 11. Minors

OronBox does not intentionally target minors. Minors should use account authorization, publishing, and device-impacting functions under guardian guidance.

## 12. Changes and contact

New purposes, broader OAuth scopes or disclosures, or a changed primary storage location require an updated notice and renewed confirmation where necessary.

- Operator: OronBox Operations Team
- Privacy and data requests: `t3164473115@163.com`
- Discussions: <https://github.com/zxor-org/OronBox/discussions>
