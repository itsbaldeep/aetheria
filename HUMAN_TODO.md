# HUMAN_TODO — tasks only the human can do

The agent adds a line here whenever it needs something it cannot legally
obtain or decide itself. It parks the item and continues with placeholders —
never idles. Check off items as you provide them.

## Art / assets
- [ ] **VRoid character models (2–4 .vrm files)** — male/female per class
      silhouette (Blade Dancer, Spellweaver). Export from VRoid Studio, upload
      to the VPS (e.g. `/home/agency/projects/aetheria/client/assets/characters/vrm/`).
      Until then: capsule placeholders, VRM pipeline + toon shader code is scaffolded.
- [ ] **Mixamo animations** — idle, run, jump, 2–3 attacks, cast, hit, death.
      Retarget onto VRM models once supplied. (Placeholders until then.)
- [ ] **CC0/CC-BY environment & mob packs** — Kenney / Quaternius / KayKit /
      Poly Pizza. Agent may source these itself (auto-download, CREDITS.md);
      this item only matters if you'd rather pick specific packs.

## Domain
- [ ] (Post-MVP) Decide on a dedicated domain for the live game; register at
      Hostinger, add A record + wildcard. Currently on aetheria.apps.deployden.tech.

## Payments (M9)
- [ ] (Later) Real Stripe/PayPal keys when donations go live. MVP ships with
      the sandbox/mock provider + manual-approval flow.

## Account
- [ ] (M1) First GM/admin account email: **itsbaldeep@gmail.com** (locked).
      TOTP secret generated at admin setup — will be shared securely then.

## Ops
- [ ] (M10) Off-site backup target for `deploy/backup.sh` (rclone bucket or
      laptop over tailnet).

## Naming
- [ ] (Anytime) Confirm final game name if "Aetheria" isn't final — branding.json.
