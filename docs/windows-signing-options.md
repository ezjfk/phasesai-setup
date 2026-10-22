# Windows signing options

Checked 22 October 2026 against the providers' own documentation. This is research only: no application was submitted and no signing account, certificate, or workflow was configured.

## Practical answer

**Potentially free: apply to SignPath Foundation for the open-source installer. Approval is required.** A self-signed certificate is free but does not give a fresh Windows installation a trusted publisher. Signing also does not guarantee that every new download avoids SmartScreen. [SignPath Foundation](https://signpath.org/), [Microsoft signing options](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options), [Microsoft SmartScreen guidance](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).

## Options

| Option | Current cost / condition | Fit for these downloadable EXEs |
| --- | --- | --- |
| SignPath Foundation | Free for accepted open-source projects; certificate names the Foundation | The main free candidate; acceptance is discretionary. [Program](https://signpath.org/) |
| Microsoft Artifact Signing, formerly Trusted Signing | Basic: $9.99/account/month for 5,000 signatures; $0.005 per extra signature | A paid alternative with CI integration. Free, trial, and sponsored Azure subscriptions are unsupported. [Pricing tiers](https://learn.microsoft.com/en-us/azure/artifact-signing/how-to-change-sku), [Subscription FAQ](https://learn.microsoft.com/en-us/azure/artifact-signing/faq) |
| Certum open-source certificate | Shop lists a starting price of €49 and currently shows out of stock | Discounted, not free. Individual applicants only; Certum says commercially distributed software can cause revocation. [Shop](https://shop.certum.eu/open-source-code-signing-on-simplysign.html), [Requirements](https://support.certum.eu/en/code-signing-required-documents/) |
| SSL.com | Paid IV/OV/EV products | No current free OSS Authenticode program was established from its official catalog. A free tool, consultation, or eSigner trial is not a free production certificate. [Catalog](https://www.ssl.com/products/software-integrity/code-signing/), [Ordering details](https://www.ssl.com/how-to/ordering-process-for-code-and-document-signing-certificates/) |
| Microsoft Store MSIX | Microsoft signs accepted MSIX submissions without a separate certificate purchase | Requires a different packaging/distribution path. Store submission of an existing EXE/MSI still requires the publisher's own trusted signature; it is not a free signature for the EXEs on GitHub/R2. [Microsoft signing options](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options) |
| Self-signed certificate | Free | Windows does not trust it by default. Installing a custom trust root on every customer's PC defeats the fresh-install goal. [Microsoft signing options](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options) |

## SignPath eligibility for PhasesAI Setup

The [installer license](../LICENSE) is MIT. Published Foundation conditions require an active, released, documented project with OSI-approved components, no commercial dual licensing, and no proprietary components except system libraries. They also require verifiable source builds, project reputation, MFA, team roles, a signing/privacy policy, uninstall guidance, appropriate system-change notices, product/version metadata restrictions, and manual approval for every release. Only the project's own binaries may be signed; downloaded harnesses must retain their upstream signing arrangements. [Foundation conditions](https://signpath.org/terms.html).

**Eligibility remains unconfirmed.** The published conditions checked do not state a blanket ban on an OSS installer associated with a paid hosted service. They do exclude commercial dual licensing and proprietary components. PhasesAI's commercial API relationship should be disclosed to SignPath instead of assuming that an MIT license guarantees acceptance. The embedded application form was not submitted or fully evaluated. [Conditions](https://signpath.org/terms.html), [Application](https://signpath.org/apply.html).

Maintainer input needed before applying: the responsible contact, repository/team ownership, MFA status, approver identities, evidence of adoption, and a complete description of the commercial service relationship. The current README/workflow inspection did not establish a signing policy, uninstall instructions, or Windows product/version resource metadata. Those should be addressed if pursuing this route; the research does not claim approval.

For the paid Microsoft alternative, eligibility also depends on identity and location: organizations in the USA, Canada, EU, or UK; individuals in the USA or Canada. Microsoft must validate the identity. [Microsoft eligibility](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options).

## Existing provenance and future release order

The existing [release workflow](../.github/workflows/preview.yml) generates GitHub artifact attestations. GitHub documents these as evidence of where/how an artifact was built, verified using `gh attestation verify`. **Inference:** this does not create an embedded Windows Authenticode signature or a Windows-trusted publisher; those use a separate signing system. [GitHub attestation documentation](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds), [Microsoft signing options](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options).

If signing is added later, sign and timestamp the EXEs first, verify the signatures, then generate final checksums/attestations and publish those exact bytes to GitHub and R2. Signing changes the artifact bytes. A new signed release can still initially show SmartScreen until reputation accumulates. [Microsoft SmartScreen guidance](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).
