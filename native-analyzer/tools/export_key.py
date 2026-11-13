#!/usr/bin/env python3
"""Export the pinned MIT KeyNet checkpoint; verify ONNX/PyTorch output parity."""
import argparse, hashlib, json, pathlib, sys
import numpy as np
import onnx
import onnxruntime as ort
import torch
p=argparse.ArgumentParser();p.add_argument('--source',type=pathlib.Path,required=True);p.add_argument('--output',type=pathlib.Path,required=True)
a=p.parse_args();sys.path.insert(0,str(a.source.resolve()))
from model import KeyNet
model=KeyNet().eval();checkpoint=a.source/'checkpoints/keynet.pt'
model.load_state_dict(torch.load(checkpoint,map_location='cpu',weights_only=True));torch.set_num_threads(2)
a.output.parent.mkdir(parents=True,exist_ok=True)
torch.manual_seed(42)
example=torch.rand(1,1,105,100)
torch.onnx.export(model,example,str(a.output),opset_version=17,input_names=['cqt'],output_names=['key'],dynamic_axes={'cqt':{3:'frames'}},dynamo=False)
onnx.checker.check_model(onnx.load(a.output))
options=ort.SessionOptions();options.intra_op_num_threads=2;options.inter_op_num_threads=1
session=ort.InferenceSession(str(a.output),sess_options=options,providers=['CPUExecutionProvider'])
results=[]
for frames in (8,17,100,151,300):
    x=torch.rand(1,1,105,frames)
    with torch.no_grad():expected=model(x).numpy()
    actual=session.run(None,{'cqt':x.numpy()})[0]
    error=float(np.max(np.abs(expected-actual)))
    assert error<1e-4 and expected.argmax()==actual.argmax(),(frames,error)
    results.append({'frames':frames,'max_abs_error':error})
record={'source_checkpoint_sha256':hashlib.sha256(checkpoint.read_bytes()).hexdigest(),'onnx_sha256':hashlib.sha256(a.output.read_bytes()).hexdigest(),'onnx_size':a.output.stat().st_size,'opset':17,'parity':results}
a.output.with_suffix('.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps(record,indent=2))
