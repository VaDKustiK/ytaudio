#!/usr/bin/env python3
"""Generate platform icons from the user's single PNG. Requires Pillow."""
from pathlib import Path
from PIL import Image

ROOT = Path(__file__).resolve().parent.parent


def prepare_icons(output):
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    with Image.open(ROOT / "packaging/ytaudiologo.png") as source:
        image = source.convert("RGBA")
        # Keep non-square replacement artwork undistorted.
        square = Image.new("RGBA", (max(image.size),) * 2)
        square.alpha_composite(image, ((square.width-image.width)//2, (square.height-image.height)//2))
        square.resize((512, 512), Image.Resampling.LANCZOS).save(output / "ytaudio.png")
        square.resize((256, 256), Image.Resampling.LANCZOS).save(output / "icon.ico", sizes=[(n,n) for n in (16,24,32,48,64,128,256)])
        square.resize((1024, 1024), Image.Resampling.LANCZOS).save(output / "icon.icns")
    return output


if __name__ == "__main__":
    prepare_icons(ROOT / "dist/icons")
