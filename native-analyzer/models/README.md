# Canonical key model

`keynet.onnx` is the reviewed opset-17 export of the MusicalKeyCNN checkpoint
pinned in `../sources.lock.json`. Its SHA-256 is pinned in `../models.lock.json`
and `../../internal/analyzer/models.go`.

Keep this small artifact in ordinary Git so clean builds on every target use
the same verified bytes. PyTorch exports may differ at the byte level across
build environments even when their numerical outputs agree. Fresh exports go
to ignored build storage and are compared against both PyTorch and this file;
they never replace the packaged artifact automatically.

The original model comes from the MIT-licensed MusicalKeyCNN repository at the
exact revision in the source lock; its license is preserved in `LICENSE.txt`.
Model packages include that repository's
license under `licenses/MusicalKeyCNN-MIT.txt`. See `../README.md` and
`../../THIRD_PARTY_NOTICES.md` for provenance and validation details.

Replacing this artifact is an explicit model update: review its provenance,
run the export/reference tests, update both checksum locks and the model version,
and review the narrowly scoped checksum exception in `../../.gitleaks.toml`.
