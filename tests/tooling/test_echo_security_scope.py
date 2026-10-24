import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class EchoSecurityScopeTests(unittest.TestCase):
    def test_symbol_guard_detects_a_program_referencing_proxy(self) -> None:
        environment = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")
        with tempfile.TemporaryDirectory(prefix="whisper-echo-positive-") as td:
            source = Path(td) / "main.go"
            binary = Path(td) / "proxy-fixture"
            source.write_text(
                'package main\nimport ("fmt"; "github.com/labstack/echo/v5/middleware")\n'
                "var factory = middleware.ProxyWithConfig\n"
                'func main() { fmt.Printf("%p", factory) }\n'
            )

            build = subprocess.run(
                ["go", "build", "-trimpath", "-buildvcs=false", "-o", str(binary), str(source)],
                cwd=ROOT,
                env=environment,
                capture_output=True,
                timeout=120,
            )

            self.assertEqual(build.returncode, 0, "Positive advisory scope build failed")

            result = subprocess.run(
                ["go", "tool", "nm", str(binary)],
                cwd=ROOT,
                env=environment,
                capture_output=True,
                timeout=30,
            )

            self.assertEqual(result.returncode, 0, "Positive scope symbols unavailable")
            self.assertIn("github.com/labstack/echo/v5/middleware.Proxy", result.stdout.decode())

    def test_echo_proxy_is_not_linked_into_either_entrypoint(self) -> None:
        """The exact Proxy-only advisory exception expires or fails if its code is used."""
        environment = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")
        with tempfile.TemporaryDirectory(prefix="whisper-echo-scope-") as td:
            for role in ("api", "backend"):
                with self.subTest(role=role):
                    binary = Path(td) / role
                    # Identical target/source/build settings to the images, retaining only symbols.
                    build = subprocess.run(
                        [
                            "go",
                            "build",
                            "-trimpath",
                            "-buildvcs=false",
                            "-o",
                            str(binary),
                            "./cmd/" + role,
                        ],
                        cwd=ROOT,
                        env=environment,
                        capture_output=True,
                        timeout=120,
                    )

                    self.assertEqual(build.returncode, 0, "Advisory scope build failed")

                    symbols = subprocess.run(
                        ["go", "tool", "nm", str(binary)],
                        cwd=ROOT,
                        env=environment,
                        capture_output=True,
                        timeout=30,
                    )

                    self.assertEqual(symbols.returncode, 0, "Advisory scope symbols unavailable")

                    text = symbols.stdout.decode()

                    self.assertNotIn(
                        "github.com/labstack/echo/v5/middleware.Proxy",
                        text,
                        "Echo Proxy advisory exception is no longer applicable",
                    )
                    self.assertIn(
                        " main.main", text, "Missing entrypoint in native symbol evidence"
                    )


if __name__ == "__main__":
    unittest.main()
