#!/usr/bin/env python3
"""
Port forwarder for Alpha-Nexus EgoLite.
Exposes 0.0.0.0:17420 and forwards bidirectionally to 127.0.0.1:17421.
This bridges LAN access while satisfying the Go backend's loopback-only assertion
(the Go backend sees all incoming connections as originating from 127.0.0.1).
"""
import asyncio
import sys

LISTEN_HOST = "0.0.0.0"
LISTEN_PORT = 17420
TARGET_HOST = "127.0.0.1"
TARGET_PORT = 17421

async def forward(reader, writer):
    try:
        remote_reader, remote_writer = await asyncio.open_connection(TARGET_HOST, TARGET_PORT)
    except Exception as e:
        writer.close()
        await writer.wait_closed()
        return

    async def pipe(r, w):
        try:
            while not r.at_eof():
                data = await r.read(65536)
                if not data:
                    break
                w.write(data)
                await w.drain()
        except Exception:
            pass
        finally:
            try:
                w.close()
                await w.wait_closed()
            except Exception:
                pass

    await asyncio.gather(
        pipe(reader, remote_writer),
        pipe(remote_reader, writer),
        return_exceptions=True
    )

async def main():
    server = await asyncio.start_server(forward, LISTEN_HOST, LISTEN_PORT)
    addr = server.sockets[0].getsockname()
    print(f"Forwarder listening on {addr} -> {TARGET_HOST}:{TARGET_PORT}", flush=True)
    async with server:
        await server.serve_forever()

if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
