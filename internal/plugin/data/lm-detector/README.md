# LM Fingerpoint Detector

Source: https://github.com/Ikaleio/lm-detector (MIT)

The ranker, sequence check and calibration are Go ports of upstream
`shared/shared-detector.ts`, `shared/positional.ts` and
`shared/fingerprint-core.js`, pinned at
`a4ef212df06817e8071acd05f00219f4c9eb2f9a`. The license is embedded in the plugin.

- `shared_detector.bin`: the frozen ranker and calibration, copied unchanged.
- `enrollment-suite.json`: the 36 fixed challenges the reference answers were
  collected with, copied unchanged. Each run draws three of them.
- `models.json`: the candidate names and families from upstream
  `data/unified_bank.json`, which the detector binds to by `built_at`, model
  order and response counts:

  ```sh
  jq '{built_at, models: [.models[] | {id, display_name, family, family_name, response_count}]}' \
    data/unified_bank.json > models.json
  ```

Update the three files from the same upstream commit. The bank enrolls
gpt-6-astra and gpt-6.1-sol as one candidate, `gpt-6-astra`. History records
identify the detector by its SHA-256 digest.

Probabilities compare only the included candidates. Collection conditions and
similar model behavior can affect attribution; results do not prove identity.
