# Attribution — FRP protocol messages

`msg.go` and `ctl.go` are adapted from Hugging Face's Gradio-compatible
FRP fork (https://github.com/huggingface/frp), commit
`43a1bfcf97e3f6b52e9cbe70edda55bc1743d9ab`, based on fatedier/frp.
They retain copyright headers and the Apache License 2.0 (see `LICENSE`).

The protocol integration in `../gradiolive/native_client.go` is adapted
from the `gradio_native.go` implementation in the user's Goradio project.
Goradio was used exclusively as a read-only reference. KagMCP has its own
copy and must not be assumed to track Goradio updates automatically.
