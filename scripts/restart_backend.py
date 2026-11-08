#!/usr/bin/env python3
"""Restart the dev backend binary in a detached session, preserving DQ_* env.

用得上的原因：这台机器没有 setsid，`nohup ... &` 会被调用方的进程组回收。
后端必须活过本次 tool 调用，所以走 double-fork + os.setsid()。
"""
import os

root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
binary = os.path.join(root, "out/run/backend-bin")
log = os.path.join(root, "out/run/backend.log")
pid_file = os.path.join(root, "out/run/backend.pid")

env = dict(os.environ)
env.update({
    "DQ_ROOT": root,
    "DQ_OUT": os.path.join(root, "out"),
    "DQ_DEV": "danmo-novel",
    "DQ_PROJECT": "danmo-novel",
    "DQ_DEV_ROOT": root,
    "DQ_DEV_ROLE": "backend",
    "DQ_DEV_MARKER": f"danmo-novel@{root}",
    "DQ_BACKEND_PORT": "7804",
    "DQ_RUN_DIR": os.path.join(root, "out/run"),
    "DQ_APP_NAME": "danmo-novel",
    "DQ_HTTP_HOST": "127.0.0.1",
})

if os.fork() > 0:
    os._exit(0)
os.setsid()
if os.fork() > 0:
    os._exit(0)

fd = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o644)
os.dup2(fd, 1)
os.dup2(fd, 2)
devnull = os.open(os.devnull, os.O_RDONLY)
os.dup2(devnull, 0)

with open(pid_file, "w", encoding="utf-8") as fh:
    fh.write(str(os.getpid()))

os.execve(binary, [binary], env)
