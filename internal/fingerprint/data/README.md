# LM Fingerpoint Detector

The scoring code and embedded data are pinned to
[Ikaleio/lm-detector at 33fb43f2](https://github.com/Ikaleio/lm-detector/tree/33fb43f26670be0ed7817ab13e5c4c4731ccb76a)
under the MIT license. The plugin embeds the license in its management page.

- `shared_detector.bin` and `enrollment-suite.json` come from upstream `data/`.
- `models.json` contains the metadata used by the detector. Generate it from the same
  commit as the binary, preserving the model order and response counts:

  ```sh
  jq '{built_at, models: [.models[] | {id, display_name, family, family_name, response_count}]}' \
    data/unified_bank.json > models.json
  ```

The conformance cases in `../testdata/analyze.json` come from
`sdk/conformance/analyze.json`. Each record stores the detector binary's SHA-256.
