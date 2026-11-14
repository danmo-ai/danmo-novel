#!/usr/bin/env python3
"""Render the Danmo Novel app icon and every size the native apps need.

The visual language is borrowed from DanQing Studio's icon so the two apps read
as one family: a full-bleed deep-ink ground and the three traditional pigments
(朱砂 vermilion, 石青 azurite, 赭石 ochre). Danmo Novel is 丹墨 — "cinnabar ink" —
so the mark is a book whose spine carries the cinnabar.

Everything is drawn at 4x and downsampled once, so edges are anti-aliased
without a vector toolkit. Run from the repository root:

    python3 scripts/make_app_icon.py

Outputs:
    native-ios/Resources/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png
    native-android/app/src/main/res/mipmap-*/ic_launcher*.png
    native-android/app/src/main/res/drawable/ic_launcher_foreground.xml
    desktop/src-tauri/icons/*  (PNG set; .icns/.ico are rebuilt by Tauri)
"""

from __future__ import annotations

import math
import shutil
import subprocess
import tempfile
from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent.parent

# Pigments shared with DanQing Studio, sampled from its shipped icon.
INK = (58, 22, 32)          # #3A1620  deep plum ink ground
VERMILION = (201, 55, 86)   # #C93756  朱砂 — the cinnabar spine
AZURITE = (42, 122, 130)    # #2A7A82  石青 — the cover board
OCHRE = (200, 172, 128)     # #C8AC80  赭石 — page edge warmth

# Paper is warmed ochre, not pure white: on a plum ground a cream page reads as
# paper, while white reads as a hole punched in the icon.
PAPER = (242, 230, 208)     # #F2E6D0
PAPER_SHADE = (223, 203, 172)

SS = 4                      # supersample factor
S = 1024                    # master size


def lerp(a, b, t):
    return tuple(round(a[i] + (b[i] - a[i]) * t) for i in range(3))


def rounded(draw, box, r, fill):
    draw.rounded_rectangle(box, radius=r, fill=fill)


def ground(size: int) -> Image.Image:
    """Full-bleed ink ground with a soft top-left lift.

    A flat fill looks dead at 1024pt and muddy at 20pt; a very low-contrast
    diagonal wash keeps depth without reading as a gradient.
    """
    img = Image.new("RGB", (size, size), INK)
    px = img.load()
    light = lerp(INK, (76, 34, 48), 1.0)
    for y in range(size):
        for x in range(size):
            # Diagonal falloff, strongest at the top-left.
            d = (x + y) / (2 * size)
            t = max(0.0, 1.0 - d * 1.55) ** 2
            px[x, y] = lerp(INK, light, t * 0.55)
    return img


def page_polygon(side: str) -> list[tuple[float, float]]:
    """One page of an open book, in master coordinates * SS.

    The gutter sits lower than the outer edge, which is what makes two
    parallelograms read as an open book rather than as a folded card. The top
    edge bows upward slightly for the same reason.
    """
    left = 214.0
    right = 810.0
    gutter_x = 512.0
    half_gap = 17.0
    top_outer, top_inner = 300.0, 366.0
    bot_outer, bot_inner = 658.0, 724.0
    bow = 22.0

    if side == "left":
        outer_x = left
        inner_x = gutter_x - half_gap
    else:
        outer_x = right
        inner_x = gutter_x + half_gap

    def top_at(s: float) -> tuple[float, float]:
        x = outer_x + (inner_x - outer_x) * s
        y = top_outer + (top_inner - top_outer) * s - bow * math.sin(math.pi * s)
        return (x, y)

    def bottom_at(s: float) -> tuple[float, float]:
        # Mirrored: the gutter is the low point at the top and the high point at
        # the bottom, which is what makes the two pages look like a spread seen
        # from slightly above rather than two tilted cards.
        x = inner_x + (outer_x - inner_x) * s
        y = bot_inner + (bot_outer - bot_inner) * s + bow * 0.4 * math.sin(math.pi * s)
        return (x, y)

    # Outer corners read as paper and get the larger radius; inner corners sit
    # against the spine and stay tight.
    r_outer, r_inner = 26.0, 10.0
    top_len = math.dist((outer_x, top_outer), (inner_x, top_inner))
    bot_len = math.dist((outer_x, bot_outer), (inner_x, bot_inner))
    side_len = bot_inner - top_inner

    def sample(fn, s0, s1, n=20):
        return [fn(s0 + (s1 - s0) * i / n) for i in range(n + 1)]

    outline: list[tuple[float, float]] = []
    # Top edge: outer -> inner, trimmed at both corners. Starting at 0.0 would
    # re-insert the sharp outer corner and leave a wedge cut by the arc.
    outline += sample(top_at, r_outer / top_len, 1.0 - r_inner / top_len, 26)
    # Round inner-top.
    c = top_at(1.0)
    outline += _arc(top_at(1.0 - r_inner / top_len), c, (inner_x, top_inner + r_inner), 8)
    # Inner edge down to the bottom corner.
    outline += [(inner_x, y) for y in
                _linspace(top_inner + r_inner, bot_inner - r_inner, 14)]
    # Round inner-bottom.
    c = (inner_x, bot_inner)
    outline += _arc((inner_x, bot_inner - r_inner), c,
                    _trim(bottom_at, 0.0, r_inner / bot_len), 8)
    # Bottom edge: inner -> outer.
    outline += sample(bottom_at, r_inner / bot_len, 1.0 - r_outer / bot_len, 26)
    # Round outer-bottom.
    c = bottom_at(1.0)
    outline += _arc(_trim(bottom_at, 1.0, r_outer / bot_len), c,
                    (outer_x, bot_outer - r_outer), 8)
    # Outer edge up to the outer-top corner.
    outline += [(outer_x, y) for y in
                _linspace(bot_outer - r_outer, top_outer + r_outer, 14)]
    # Round outer-top.
    c = (outer_x, top_outer)
    outline += _arc((outer_x, top_outer + r_outer), c,
                    top_at(r_outer / top_len), 8)

    return [(x * SS, y * SS) for x, y in outline]


def _linspace(a: float, b: float, n: int) -> list[float]:
    return [a + (b - a) * i / n for i in range(n + 1)]


def _trim(fn, s: float, d: float):
    """Step `d` (a fraction of the edge) inward from parameter `s`."""
    return fn(max(0.0, min(1.0, s - d)) if s > 0.5 else min(1.0, s + d))


def _arc(a, corner, b, steps: int) -> list[tuple[float, float]]:
    """Quadratic Bezier that cuts a corner, sampled into a polyline."""
    out = []
    for i in range(1, steps):
        t = i / steps
        x = (1 - t) ** 2 * a[0] + 2 * (1 - t) * t * corner[0] + t**2 * b[0]
        y = (1 - t) ** 2 * a[1] + 2 * (1 - t) * t * corner[1] + t**2 * b[1]
        out.append((x, y))
    out.append(b)
    return out


def draw_master(size: int = S) -> Image.Image:
    big = size * SS
    img = ground(big).convert("RGB")
    d = ImageDraw.Draw(img)

    # --- pages -------------------------------------------------------------
    left_page = page_polygon("left")
    right_page = page_polygon("right")

    # --- paper thickness: an ochre sheet line under the pages --------------
    # A book seen from above still shows the cut edge of its block. One thin
    # line does that job; three would turn to grey mush when scaled down.
    for page in (left_page, right_page):
        d.polygon(
            [(x, y + 14 * SS) for x, y in page]
            + [(x, y + 3 * SS) for x, y in reversed(page)],
            fill=OCHRE,
        )

    d.polygon(left_page, fill=PAPER)
    # The leaf falling away from the light gets a touch of shade, so the spread
    # reads as two planes. Kept subtle: a strong split looks like one page is
    # greyed out rather than lit differently.
    d.polygon(right_page, fill=lerp(PAPER, PAPER_SHADE, 0.55))

    # --- cinnabar spine ----------------------------------------------------
    # The mark's only saturated vertical: it names the app (丹) and doubles as
    # the fold between the pages. It runs the full height of the gutter, which
    # is what makes the two pages read as one bound book.
    d.rounded_rectangle(
        [512 * SS - 8 * SS, 372 * SS, 512 * SS + 8 * SS, 730 * SS],
        radius=8 * SS, fill=VERMILION,
    )

    # --- text rules on the left page --------------------------------------
    # Azurite is the ink: 石青 on paper. Two short rules, not five — at 20pt
    # anything finer turns to noise, and two strokes still say "written page".
    rule_left = 284.0
    rule_right = 512.0 - 46.0
    for y, wfrac in [(0.452, 0.9), (0.552, 0.62)]:
        y0 = y * size
        x1 = rule_left + (rule_right - rule_left) * wfrac
        d.rounded_rectangle(
            [rule_left * SS, (y0 - 8) * SS, x1 * SS, (y0 + 8) * SS],
            radius=8 * SS, fill=lerp(AZURITE, PAPER, 0.12),
        )

    return img.resize((size, size), Image.LANCZOS)


# --- writers ---------------------------------------------------------------

def write_ios(master: Image.Image) -> None:
    out = ROOT / "native-ios/Resources/Assets.xcassets/AppIcon.appiconset"
    out.mkdir(parents=True, exist_ok=True)
    master.save(out / "AppIcon-1024.png")
    (out / "Contents.json").write_text(
        """{
  "images" : [
    {
      "filename" : "AppIcon-1024.png",
      "idiom" : "universal",
      "platform" : "ios",
      "size" : "1024x1024"
    }
  ],
  "info" : {
    "author" : "xcode",
    "version" : 1
  }
}
""",
        encoding="utf-8",
    )
    # The catalog itself needs a root Contents.json.
    (out.parent / "Contents.json").write_text(
        '{\n  "info" : {\n    "author" : "xcode",\n    "version" : 1\n  }\n}\n',
        encoding="utf-8",
    )


ANDROID_DENSITIES = {
    "mipmap-mdpi": 48,
    "mipmap-hdpi": 72,
    "mipmap-xhdpi": 96,
    "mipmap-xxhdpi": 144,
    "mipmap-xxxhdpi": 192,
}


def write_android(master: Image.Image) -> None:
    res = ROOT / "native-android/app/src/main/res"
    for folder, px in ANDROID_DENSITIES.items():
        d = res / folder
        d.mkdir(parents=True, exist_ok=True)
        master.resize((px, px), Image.LANCZOS).save(d / "ic_launcher.png")
        # Legacy round mask: identical art, system applies the circle.
        master.resize((px, px), Image.LANCZOS).save(d / "ic_launcher_round.png")

    # Adaptive icon: the launcher crops to its own shape and can parallax the
    # layers, so the mark must sit inside the 66dp safe circle of a 108dp
    # canvas, and the ground becomes a flat colour layer.
    fg = Image.new("RGBA", (432, 432), (0, 0, 0, 0))
    inner = int(432 * 0.70)
    inner_img = master.convert("RGBA").resize((inner, inner), Image.LANCZOS)
    off = (432 - inner) // 2
    fg.paste(inner_img, (off, off))
    # Convert the ground to alpha so the background layer shows through.
    mark = Image.new("RGBA", (432, 432), (0, 0, 0, 0))
    src, dst = fg.load(), mark.load()
    for y in range(432):
        for x in range(432):
            r, g, b, a = src[x, y]
            if a and abs(r - INK[0]) + abs(g - INK[1]) + abs(b - INK[2]) < 90:
                continue
            dst[x, y] = (r, g, b, 255)

    d = res / "drawable"
    d.mkdir(parents=True, exist_ok=True)
    mark.save(d / "ic_launcher_foreground.png")

    # Themed icons (Android 13+) tint a monochrome layer; keep the alpha and
    # drop the colour so the launcher can recolour it.
    mono = Image.new("RGBA", (432, 432), (0, 0, 0, 0))
    mono.putalpha(mark.getchannel("A"))
    white = Image.new("RGBA", (432, 432), (255, 255, 255, 0))
    white.putalpha(mark.getchannel("A"))
    white.save(d / "ic_launcher_monochrome.png")

    anydpi = res / "mipmap-anydpi-v26"
    anydpi.mkdir(parents=True, exist_ok=True)
    (anydpi / "ic_launcher.xml").write_text(
        """<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background" />
    <foreground android:drawable="@drawable/ic_launcher_foreground" />
    <monochrome android:drawable="@drawable/ic_launcher_monochrome" />
</adaptive-icon>
""",
        encoding="utf-8",
    )
    (anydpi / "ic_launcher_round.xml").write_text(
        (anydpi / "ic_launcher.xml").read_text(encoding="utf-8"), encoding="utf-8"
    )
    (res / "values" / "colors.xml").write_text(
        """<?xml version="1.0" encoding="utf-8"?>
<resources>
    <!-- Same ink ground as the iOS icon, so the two apps match on a home screen. -->
    <color name="ic_launcher_background">#3A1620</color>
</resources>
""",
        encoding="utf-8",
    )


def write_desktop(master: Image.Image) -> None:
    icons = ROOT / "desktop/src-tauri/icons"
    if not icons.is_dir():
        return
    # Tauri refuses a PNG that is not RGBA ("icon ... is not RGBA"), and the
    # renderer works in RGB. Convert once here so every size, the .ico and the
    # .icns come out RGBA without a per-file branch.
    master = master.convert("RGBA")
    for name, px in [
        ("32x32.png", 32),
        ("64x64.png", 64),
        ("128x128.png", 128),
        ("128x128@2x.png", 256),
        ("icon.png", 512),
    ]:
        master.resize((px, px), Image.LANCZOS).save(icons / name)

    # Windows uses multi-resolution .ico, macOS a container of PNGs.
    master.save(icons / "icon.ico", sizes=[
        (16, 16), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256),
    ])

    ios = icons / "ios"
    if ios.is_dir():
        for base, scale in [
            (20, 1), (20, 2), (20, 3), (29, 1), (29, 2), (29, 3),
            (40, 1), (40, 2), (40, 3), (60, 2), (60, 3),
            (76, 1), (76, 2), (83.5, 2), (1024, 1),
        ]:
            px = int(round(base * scale))
            master.resize((px, px), Image.LANCZOS).save(
                ios / f"AppIcon-{base:g}x{base:g}@{scale}x.png"
            )
        master.resize((1024, 1024), Image.LANCZOS).save(ios / "AppIcon-512@2x.png")

    # .icns is only buildable on macOS via iconutil. Failing to rebuild it must
    # not fail the whole run: the PNG set alone is enough for dev builds.
    if shutil.which("iconutil"):
        with tempfile.TemporaryDirectory() as td:
            iconset = Path(td) / "icon.iconset"
            iconset.mkdir()
            for base in (16, 32, 128, 256, 512):
                for scale in (1, 2):
                    px = base * scale
                    suffix = "" if scale == 1 else "@2x"
                    master.resize((px, px), Image.LANCZOS).save(
                        iconset / f"icon_{base}x{base}{suffix}.png"
                    )
            subprocess.run(
                ["iconutil", "-c", "icns", str(iconset), "-o", str(icons / "icon.icns")],
                check=False,
                capture_output=True,
            )


def write_preview(master: Image.Image) -> None:
    """Contact sheet so the mark can be judged at real sizes."""
    sizes = [256, 180, 120, 80, 40, 20]
    pad = 24
    width = sum(sizes) + pad * (len(sizes) + 1)
    height = 256 + pad * 2
    sheet = Image.new("RGB", (width, height), (244, 244, 246))
    x = pad
    for s in sizes:
        sheet.paste(master.resize((s, s), Image.LANCZOS), (x, pad + (256 - s) // 2))
        x += s + pad
    out = ROOT / "out"
    out.mkdir(exist_ok=True)
    sheet.save(out / "app-icon-preview.png")


def main() -> None:
    master = draw_master()
    write_ios(master)
    write_android(master)
    write_desktop(master)
    write_preview(master)
    print("wrote app icon set")


if __name__ == "__main__":
    main()
