---
name: drive-raw-mode-apps-through-a-pty
description: Verify serve's terminal input from a non-TTY reviewer shell by forking under pty.openpty, and prove terminal restore by comparing the slave's termios before and after
metadata:
  type: feedback
---

To verify Channel Three's terminal key source, fork the built binary under `pty.openpty()` in `python3`, keep the slave fd open in the parent, and compare `termios.tcgetattr(slave)` before the run with after the process exits. Equality is the proof that raw mode was restored; the control-character array catches a partial restore that the flag words alone would miss.

**Why:** A reviewer session's shell is not a TTY, so `serve` skips its terminal source entirely and an interactive checklist item cannot be exercised at all without a pty. Trusting the executor's own transcript for it means verifying nothing. A `stty -g` comparison is not available either, because the terminal being restored belongs to the forked child, not to the reviewer's shell.

**How to apply:** `os.setsid()` in the child then `dup2` the slave onto 0, 1 and 2. Drive keys with `os.write(master, b"+")` and read with `select` on the master, never a blocking read. Send `\x03` to exit and `os.waitpid` with `WNOHANG` against a deadline, with a `SIGKILL` fallback, so a hung binary cannot park the review. Escape sequences go as raw bytes: `b"\x1b[A"` is the up arrow. Build the whole script in the scratchpad and run it against [[manual-verification-root]]; `serve` writes only its pid file there and removes it on exit, so the root stays byte-identical. Pair this with [[verify-without-touching-the-tree]]: the pty proves what happens on air, the overlay probe proves what happens in the package.
