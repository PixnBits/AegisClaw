# User Personas

## Primary Users

### 1. Paranoid Power User
**Description:** Technical individual who deeply cares about security and privacy. Runs their own local models and refuses to send sensitive data to cloud providers.

**Goals:**
- Complete control over their data and secrets
- High confidence that no skill can compromise their system
- Ability to safely extend the agent's capabilities over time

**Pain Points:**
- Existing local agents feel too trusting and opaque
- Cloud-based agents are a non-starter due to privacy concerns

### 2. Security-Conscious Developer
**Description:** Software engineer or security researcher who wants to experiment with agentic workflows but maintains strict security standards.

**Goals:**
- Safe environment to test and run untrusted agent code
- Clear audit trail of everything the agent does
- Easy way to add custom skills with proper review gates

### 3. Small Team / Indie Hacker
**Description:** Small team or solo founder who wants powerful automation without relying on SaaS tools that lock them in or expose their data.

**Goals:**
- Reduce dependency on multiple paid SaaS tools
- Keep business logic and API keys private
- Have an agent that can grow with their needs

## Secondary Users

### 4. Enterprise Security & Platform Teams

Large organizations interested in deploying AegisClaw across many machines.

**Key Requirements:**
- **Centralized Policy Management** — Define security policies once and push them to all instances
- **Configurable Governance** — Ability to customize Court personas, voting thresholds, and required reviewers per policy
- **Enterprise Audit & Compliance** — Integration with SIEM systems and standardized log formats
- **Identity & Access Management** — Support for enterprise identity providers and role-based permissions
- **Supply Chain Security** — Ability to pin and verify all base images, models, and dependencies
- **Controlled Skill Distribution** — Ability to approve and distribute approved skills across the organization
- **Resource Governance** — Control over compute and memory usage per agent

These teams are primarily concerned with **governance at scale**, compliance, and maintaining consistent security posture across their fleet.
