import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
FAKE_GH = """import json
import os
import shutil
import sys
from pathlib import Path

remote = Path(os.environ["ASSET_REMOTE"])
args = sys.argv[1:]
operation = args[1]
if operation == "view":
    if not (remote / "release.json").exists():
        sys.exit(1)
    release = json.loads((remote / "release.json").read_text())
    release["assets"] = [{"name": item.name} for item in (remote / "assets").iterdir()]
    print(json.dumps(release))
elif operation == "create":
    (remote / "release.json").write_text(json.dumps({"isPrerelease": "--prerelease" in args}))
elif operation == "download":
    name = args[args.index("--pattern") + 1]
    directory = Path(args[args.index("--dir") + 1])
    shutil.copyfile(remote / "assets" / name, directory / name)
elif operation == "upload":
    source = Path(args[-1])
    destination = remote / "assets" / source.name
    if destination.exists():
        sys.exit(1)
    shutil.copyfile(source, destination)
else:
    sys.exit(2)
"""


class ReleaseAssetsTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.remote = self.root / "remote"
        (self.remote / "assets").mkdir(parents=True)
        self.chart_dir = self.root / "chart"
        self.chart_dir.mkdir()
        (self.chart_dir / "whisper-server-0.1.0-rc.1.tgz").write_bytes(b"selected chart")
        self.state = self.root / "channel-state.json"
        self.state.write_text('{"qualified":true}\n')
        binary = self.root / "gh"
        binary.write_text(f"#!{sys.executable}\n" + FAKE_GH)
        binary.chmod(0o700)

    def publish(self) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash", str(ROOT / "tools/release/upload-assets.sh")],
            env=dict(
                os.environ,
                PATH=str(self.root) + os.pathsep + os.environ["PATH"],
                ASSET_REMOTE=str(self.remote),
                RUNNER_TEMP=str(self.root),
                RELEASE_TAG="v0.1.0-rc.1",
                RELEASE_STAGE="candidate",
                STATE_FILE=str(self.state),
                CHART_DIR=str(self.chart_dir),
            ),
            capture_output=True,
            text=True,
            timeout=30,
        )

    def test_upload_creates_prerelease_and_identical_retry_does_not_overwrite(self) -> None:
        first = self.publish()
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertTrue(json.loads((self.remote / "release.json").read_text())["isPrerelease"])
        self.assertEqual(
            (self.remote / "assets" / self.state.name).read_bytes(), self.state.read_bytes()
        )
        second = self.publish()
        self.assertEqual(second.returncode, 0, second.stderr)

    def test_existing_different_asset_stops_without_overwriting(self) -> None:
        (self.remote / "release.json").write_text('{"isPrerelease":true}')
        existing = self.remote / "assets" / self.state.name
        existing.write_bytes(b"previous immutable bytes")
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(existing.read_bytes(), b"previous immutable bytes")

    def test_wrong_release_mode_stops_before_upload(self) -> None:
        (self.remote / "release.json").write_text('{"isPrerelease":false}')
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(list((self.remote / "assets").iterdir()), [])
