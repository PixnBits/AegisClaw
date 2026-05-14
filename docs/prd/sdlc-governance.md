# SDLC & Governance Integration

No code change — regardless of size — may be made to the system without formal approval from the Governance Court.

## Core Principle

**Every change must be reviewed.** Even a one-line edit or single character change can introduce serious security issues.

## The SDLC Process

1. **Proposal**  
   A formal Change Proposal is created (new skill, bugfix, prompt change, etc.).

2. **Court Review**  
   The five-persona Governance Court reviews the proposal. All personas participate unless explicitly configured otherwise.

3. **Implementation**  
   Approved changes are implemented inside isolated build microVMs.

4. **Testing & Validation**  
   Automated tests plus manual validation where required.

5. **Court Sign-off**  
   The Court performs a final review of the implemented changes before they are allowed to be deployed.

6. **Deployment**  
   Only after final Court approval is the change merged and activated.

## Scope of Review

The Court must review **all** of the following:
- Creation or modification of any skill
- Changes to any agent prompt or `soul.md`
- Changes to core components (including the daemon)
- Any modification to the Governance Court itself
- Changes to autonomy levels or security policies

## Configurability

Enterprise users and advanced users may configure which Court personas are required for different types of changes (e.g. bugfixes may skip the User Advocate, while new skills require full Court review).

