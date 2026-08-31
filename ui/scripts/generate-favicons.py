#!/usr/bin/env python3
"""Generate the browser icons in ui/public.

Run from anywhere after changing ui/src/assets/logo.png:

    python3 -m pip install pillow
    python3 ui/scripts/generate-favicons.py

The set is optically sized, because the logo does not survive being squeezed
into a tab:

* Tab-sized icons keep the two rings and drop the CH/UI lettering, which blurs
  into the rings below roughly 48px. The rings are also drawn thicker than in
  the logo — its strokes are ~6% of the width, which lands on a single pixel at
  16px and looks frayed — and are emitted as SVG as well, so browsers that
  prefer a vector icon stay sharp at any scale.
* The full mark is used from 192px up, where the lettering reads: Android home
  screens, PWA install prompts and the iOS touch icon. It is trimmed of the
  ~10px of empty margin the logo carries, which is wasted space in an icon.

Because the rings are drawn rather than scaled out of the artwork, their colours
are kept in sync with the logo here by hand: #ffba00 and #e80909.
"""
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "ui" / "src" / "assets" / "logo.png"
OUT = ROOT / "ui" / "public"

YELLOW_HEX = "#ffba00"
RED_HEX = "#e80909"
YELLOW = (255, 186, 0, 255)
RED = (232, 9, 9, 255)

# Ring stroke widths and the gap between them, as a fraction of the icon.
YELLOW_STROKE = 0.12
RING_GAP = 0.05
RED_STROKE = 0.12

# Rings are drawn large and scaled down, which antialiases the curves.
SUPERSAMPLE = 16

ICO_SIZES = [32, 48]
RINGS_PNG_SIZES = [16, 32]
FULL_MARK_PNG_SIZES = [192, 512]
APPLE_SIZE = 180

# iOS drops transparency and composites onto black, which would leave the
# orange lettering floating in a black disc, so that icon gets a white plate.
APPLE_BACKGROUND = (255, 255, 255, 255)
APPLE_LOGO_SCALE = 0.82

SHARPEN = ImageFilter.UnsharpMask(radius=1, percent=120, threshold=2)


def load_trimmed() -> Image.Image:
    """The logo with its empty margin removed."""
    logo = Image.open(SOURCE).convert("RGBA")
    box = logo.getchannel("A").getbbox()
    return logo.crop(box) if box else logo


def rings(size: int) -> Image.Image:
    big = size * SUPERSAMPLE
    canvas = Image.new("RGBA", (big, big), (0, 0, 0, 0))
    draw = ImageDraw.Draw(canvas)
    centre = big / 2

    yellow_w = YELLOW_STROKE * big
    red_w = RED_STROKE * big
    gap = RING_GAP * big

    outer_r = centre - yellow_w / 2
    draw.ellipse(
        (centre - outer_r, centre - outer_r, centre + outer_r, centre + outer_r),
        outline=YELLOW,
        width=round(yellow_w),
    )

    inner_r = centre - yellow_w - gap - red_w / 2
    draw.ellipse(
        (centre - inner_r, centre - inner_r, centre + inner_r, centre + inner_r),
        outline=RED,
        width=round(red_w),
    )

    return canvas.resize((size, size), Image.LANCZOS)


def rings_svg() -> str:
    """The same two rings as a vector, for crisp tabs at any scale."""
    box = 32.0
    yellow_w = YELLOW_STROKE * box
    red_w = RED_STROKE * box
    gap = RING_GAP * box
    outer_r = box / 2 - yellow_w / 2
    inner_r = box / 2 - yellow_w - gap - red_w / 2
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">\n'
        f'  <circle cx="16" cy="16" r="{outer_r:.3f}" fill="none"'
        f' stroke="{YELLOW_HEX}" stroke-width="{yellow_w:.3f}"/>\n'
        f'  <circle cx="16" cy="16" r="{inner_r:.3f}" fill="none"'
        f' stroke="{RED_HEX}" stroke-width="{red_w:.3f}"/>\n'
        "</svg>\n"
    )


def full_mark(logo: Image.Image, size: int) -> Image.Image:
    out = logo.resize((size, size), Image.LANCZOS)
    return out.filter(SHARPEN) if size <= 48 else out


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    logo = load_trimmed()

    (OUT / "favicon.svg").write_text(rings_svg(), encoding="utf-8")
    print("favicon.svg", "rings only")

    rings(max(ICO_SIZES)).save(OUT / "favicon.ico", sizes=[(s, s) for s in ICO_SIZES])
    print("favicon.ico", ICO_SIZES, "rings only")

    for size in RINGS_PNG_SIZES:
        name = f"favicon-{size}x{size}.png"
        rings(size).save(OUT / name)
        print(name, "rings only")

    for size in FULL_MARK_PNG_SIZES:
        name = f"favicon-{size}x{size}.png"
        full_mark(logo, size).save(OUT / name)
        print(name, "full mark")

    apple = Image.new("RGBA", (APPLE_SIZE, APPLE_SIZE), APPLE_BACKGROUND)
    inner = round(APPLE_SIZE * APPLE_LOGO_SCALE)
    offset = (APPLE_SIZE - inner) // 2
    apple.alpha_composite(full_mark(logo, inner), (offset, offset))
    apple.convert("RGB").save(OUT / "apple-touch-icon.png")
    print("apple-touch-icon.png", APPLE_SIZE, "full mark")


if __name__ == "__main__":
    main()
