#!/usr/bin/env python3
"""Generate the onlet PNG app icons with the standard library only.

Run from anywhere with `python3 -I web/scripts/gen-icons.py`. Writes web/public/icon-180.png
and web/public/icon-512.png: the mark from web/public/icon.svg (a pale loop with one lit
call-yellow segment) on solid charcoal. The PNGs are opaque RGB, which is what iOS expects for
apple-touch-icon; iOS rounds the corners itself.
"""

import math
import os
import struct
import zlib

BACKGROUND = (0x1F, 0x24, 0x22)
LOOP = (0xEE, 0xF3, 0xEF)
LIT = (0xF2, 0xC2, 0x30)
SIZES = (180, 512)

# Geometry in icon.svg's 512-unit space: both arcs share one circle and a round-capped stroke.
# Angles are degrees clockwise from 3 o'clock (SVG's y-down convention); each arc runs
# clockwise from its start for its sweep.
CENTRE = 256.0
RADIUS = 128.0
HALF_WIDTH = 22.0
ARCS = (
    (355.0, 280.0, LOOP),
    (305.0, 20.0, LIT),
)


def png_chunk(kind: bytes, data: bytes) -> bytes:
    crc = zlib.crc32(kind + data) & 0xFFFFFFFF
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", crc)


def arc_distance(x: float, y: float, start: float, sweep: float) -> float:
    """Distance from (x, y) to the arc's centre line, so a round-capped stroke is d <= HALF_WIDTH."""
    dx, dy = x - CENTRE, y - CENTRE
    if (math.degrees(math.atan2(dy, dx)) - start) % 360.0 <= sweep:
        return abs(math.hypot(dx, dy) - RADIUS)
    ends = (math.radians(start), math.radians(start + sweep))
    return min(
        math.hypot(x - (CENTRE + RADIUS * math.cos(a)), y - (CENTRE + RADIUS * math.sin(a)))
        for a in ends
    )


def render(size: int) -> bytes:
    scale = 512.0 / size  # icon units per pixel
    rows = bytearray()
    for py in range(size):
        rows.append(0)  # PNG filter type 0 (none) for each scanline
        for px in range(size):
            x, y = (px + 0.5) * scale, (py + 0.5) * scale
            colour = BACKGROUND
            for start, sweep, ink in ARCS:
                # Coverage from the distance to the stroke's edge gives one pixel of anti-aliasing.
                edge = (HALF_WIDTH - arc_distance(x, y, start, sweep)) / scale
                cover = min(max(edge + 0.5, 0.0), 1.0)
                if cover > 0:
                    colour = tuple(round(c + (i - c) * cover) for c, i in zip(colour, ink))
            rows += bytes(colour)
    header = struct.pack(">IIBBBBB", size, size, 8, 2, 0, 0, 0)  # 8-bit RGB, no interlace
    return (
        b"\x89PNG\r\n\x1a\n"
        + png_chunk(b"IHDR", header)
        + png_chunk(b"IDAT", zlib.compress(bytes(rows), 9))
        + png_chunk(b"IEND", b"")
    )


def main() -> None:
    public = os.path.join(os.path.dirname(os.path.abspath(__file__)), os.pardir, "public")
    for size in SIZES:
        path = os.path.normpath(os.path.join(public, f"icon-{size}.png"))
        with open(path, "wb") as f:
            f.write(render(size))
        print(f"wrote {path} ({size}x{size})")


if __name__ == "__main__":
    main()
