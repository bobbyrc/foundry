#!/usr/bin/env python3
"""Generate the foundry PNG app icons with the standard library only.

Run from anywhere with `python3 -I web/scripts/gen-icons.py`. Writes web/public/icon-180.png
and web/public/icon-512.png: a solid dark background with a centred amber square whose side
is 40% of the icon. The PNGs are opaque RGB, which is what iOS expects for apple-touch-icon.
"""

import os
import struct
import zlib

BACKGROUND = (0x0E, 0x0F, 0x11)
SQUARE = (0xF5, 0x9E, 0x0B)
SIZES = (180, 512)
SQUARE_FRACTION = 0.3125  # matches web/public/icon.svg (160/512)


def png_chunk(kind: bytes, data: bytes) -> bytes:
    crc = zlib.crc32(kind + data) & 0xFFFFFFFF
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", crc)


def render(size: int) -> bytes:
    side = int(size * SQUARE_FRACTION)
    offset = (size - side) // 2
    bg_row = bytes(BACKGROUND) * size
    square_px = bytes(SQUARE) * side
    rows = bytearray()
    for y in range(size):
        rows.append(0)  # PNG filter type 0 (none) for each scanline
        if offset <= y < offset + side:
            rows += bg_row[: offset * 3] + square_px + bg_row[(offset + side) * 3 :]
        else:
            rows += bg_row
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
