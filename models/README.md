# Required ML artifacts

Place the exact production artifacts here before starting Docker Compose:

- `lgbm_residual_final_seed42.onnx`
- `lgbm_late_final.onnx`
- `model_manifest.json`

These files were **not present in the uploaded archive**, so they cannot be reconstructed safely from source alone. Copy them from the project machine, where the handoff records the canonical directory as:

```text
~/projects/msgr-hack/artifacts/mosgortrans_ml_FINAL/onnx/
```

The ML runtime validates the manifest feature order and, when present, model SHA-256 values at startup. Do not substitute a different model bundle without updating and revalidating the manifest.
