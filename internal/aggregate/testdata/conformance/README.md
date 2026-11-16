# Conformance cases for `relai.yaml`

Each `NN-name.yaml` is a `relai.yaml`; `NN-name.json` is what a reader must find in it: the keys of
[`../relai-yaml.md`](../relai-yaml.md), capabilities and links as plain lists of names, empty values
left out. A reader conforms when it gives the same result on every case. `go test ./internal/index`
runs the Go reader and the Python one (`../readers/relai_yaml.py`) against them.
