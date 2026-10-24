"""Derive the brand assets from tools/brand/Patrabahok_Logo.png (the master artwork).

Writes, for the webmail skin and the admin dashboard:
  logo-mark.png       the bird + envelope only (square), for small spots
  logo-full.png       the full logo (bird + wordmark) for light backgrounds
  logo-full-dark.png  the same with the navy wordmark turned white, for dark backgrounds
  favicon.ico / apple-touch-icon.png

Run from the repo root (needs Pillow):
  docker run --rm -v "$PWD:/w" -w /w python:3.12-slim \
    sh -c "pip install -q pillow && python tools/brand/make_logos.py"
"""
from PIL import Image

SRC = "tools/brand/Patrabahok_Logo.png"
SKIN = "webmail/roundcube/skins/patrabahok/images/"
DASH = "cli/web/static/"

src = Image.open(SRC).convert("RGBA")
W, H = src.size
alpha = src.getchannel("A")


def bbox(img, threshold=24):
    return img.getchannel("A").point(lambda a: 255 if a > threshold else 0).getbbox()


def square(img, pad=0.04):
    l, t, r, b = bbox(img)
    img = img.crop((l, t, r, b))
    side = int(max(img.size) * (1 + 2 * pad))
    out = Image.new("RGBA", (side, side), (0, 0, 0, 0))
    out.paste(img, ((side - img.width) // 2, (side - img.height) // 2), img)
    return out


# The artwork sits above roughly 57% of the height, the wordmark and tagline below it.
split = int(H * 0.57)
mark = square(src.crop((0, 0, W, split)))

full = src.crop(bbox(src))

# Dark variant: the wordmark's navy (dark, saturated-blue) pixels become near-white.
dark = full.copy()
px = dark.load()
fw, fh = dark.size
word_top = int(fh * 0.66)
for y in range(word_top, fh):
    for x in range(fw):
        r, g, b, a = px[x, y]
        if a > 0 and r < 90 and g < 110 and b < 170 and (r + g + b) < 300:
            px[x, y] = (241, 243, 255, a)


def save(img, path, width):
    h = round(img.height * width / img.width)
    img.resize((width, h), Image.LANCZOS).save(path, optimize=True)


for base in (SKIN, DASH):
    save(mark, base + "logo-mark.png", 256)
    save(full, base + "logo-full.png", 440)
    save(dark, base + "logo-full-dark.png", 440)
    mark.resize((180, 180), Image.LANCZOS).save(base + "apple-touch-icon.png", optimize=True)
    mark.save(base + "favicon.ico", sizes=[(16, 16), (32, 32), (48, 48), (64, 64)])
print("mark", mark.size, "full", full.size)
