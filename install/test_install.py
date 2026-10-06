import hashlib
import io
import os
from pathlib import Path
import subprocess
import shutil
import sys
import tarfile
import tempfile
import unittest


INSTALLER = Path(__file__).with_name("install.sh")


class InstallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        fakebin = self.root / "commands"
        fakebin.mkdir()
        archive = self.root / "asset.tar.gz"
        with tarfile.open(archive, "w:gz") as package:
            payload = b"#!/bin/sh\nexit 0\n"
            info = tarfile.TarInfo("mssh")
            info.size = len(payload)
            info.mode = 0o755
            package.addfile(info, io.BytesIO(payload))
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (self.root / "asset.sha256").write_text(digest + "  mssh-linux-amd64.tar.gz\n")
        driver = fakebin / "driver"
        driver.write_text(f"#!{sys.executable}\n" + r'''
import os
from pathlib import Path
import shutil
import sys

root = Path(os.environ["INSTALL_FIXTURE"])
command = Path(sys.argv[0]).name
args = sys.argv[1:]
with (root / "calls").open("a") as log:
    log.write(command + "\n")
if command == "uname":
    print("Linux" if args == ["-s"] else "x86_64")
elif command == "id":
    print("0")
elif command == "curl":
    url = args[1]
    source = root / ("asset.sha256" if url.endswith(".sha256") else "asset.tar.gz")
    shutil.copyfile(source, args[args.index("-o") + 1])
elif command == "install":
    if args[0] == "-d":
        Path(args[1]).mkdir(parents=True, exist_ok=True)
    else:
        source, destination = Path(args[-2]), Path(args[-1])
        if str(destination).startswith("/etc/systemd/system/"):
            destination = root / "units" / destination.name
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, destination)
        os.chmod(destination, int(args[args.index("-m") + 1], 8))
elif command != "systemctl":
    raise SystemExit("unexpected command")
''')
        driver.chmod(0o755)
        for command in ("uname", "id", "curl", "install", "systemctl"):
            (fakebin / command).symlink_to(driver)
        self.env = dict(os.environ, PATH=str(fakebin) + os.pathsep + os.environ["PATH"],
                        INSTALL_FIXTURE=str(self.root), BIN_DIR=str(self.root / "bin dir"),
                        VERSION="v0.2.0", SUDO="")

    def run_installer(self, *args):
        return subprocess.run(["bash", str(INSTALLER), *args], env=self.env,
                              stdin=subprocess.DEVNULL, capture_output=True, text=True,
                              timeout=5, start_new_session=True)

    def test_server_uses_default_loopback_and_quoted_binary_path(self):
        result = self.run_installer("server")
        self.assertEqual(result.returncode, 0, result.stderr)
        unit = (self.root / "units" / "mssh-server.service").read_text()
        self.assertIn(f'ExecStart="{self.env["BIN_DIR"]}/mssh" "server"\n', unit)
        self.assertNotIn("0.0.0.0", unit)

    def test_service_paths_preserve_spaces_quotes_dollars_and_percent(self):
        result = self.run_installer("agent", "node", "--server", "localhost:8443",
                                    "--token-file", '/keys/a b"c$d%e\\f')
        self.assertEqual(result.returncode, 0, result.stderr)
        unit = (self.root / "units" / "mssh-agent.service").read_text()
        self.assertIn(r'"/keys/a b\"c$$d%%e\\f"', unit)

    def test_checksum_failure_does_not_install_binary(self):
        (self.root / "asset.sha256").write_text("0" * 64 + "  asset.tar.gz\n")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("checksum mismatch", result.stderr)
        self.assertFalse((Path(self.env["BIN_DIR"]) / "mssh").exists())

    def test_invalid_mode_and_newline_fail_before_download(self):
        for args in (("unknown",), ("server", "--host", "localhost\n[Service]")):
            with self.subTest(args=args):
                result = self.run_installer(*args)
                self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "calls").exists())

    def test_prompt_stops_without_terminal(self):
        result = self.run_installer("agent")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("pass agent flags", result.stderr)
        self.assertFalse((Path(self.env["BIN_DIR"]) / "mssh").exists())

    def test_generated_unit_passes_systemd_validation(self):
        analyzer = shutil.which("systemd-analyze")
        if analyzer is None or not sys.platform.startswith("linux"):
            self.skipTest("systemd-analyze is available on Linux CI")
        result = self.run_installer("agent", "node", "--server", "localhost:8443",
                                    "--token-file", '/keys/a b"c$d%e\\f')
        self.assertEqual(result.returncode, 0, result.stderr)
        unit = self.root / "units" / "mssh-agent.service"
        result = subprocess.run([analyzer, "verify", str(unit)], capture_output=True,
                                text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
