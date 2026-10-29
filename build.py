import os
import shutil
import subprocess
import sys

here = os.path.dirname(os.path.abspath(__file__))
os.chdir(here)


def sh(cmd, env=None):
    print("$", " ".join(cmd), flush=True)
    r = subprocess.run(cmd, env=env, capture_output=True, text=True)
    if r.stdout:
        print(r.stdout)
    if r.stderr:
        print(r.stderr)
    return r.returncode


if not shutil.which("go"):
    print("Go belum ada, install via pkg...")
    sh(["pkg", "install", "-y", "golang"])

if not shutil.which("go"):
    print("GAGAL: Go tidak bisa dipasang di Termux. Pakai GitHub Actions (.github/workflows/build.yml).")
    sys.exit(1)

# Go 1.20.x = versi terakhir yang support Windows 7. GOTOOLCHAIN memaksa pakai versi itu
# (akan didownload otomatis kalau Go di Termux lebih baru dari 1.21).
env = dict(os.environ, GOOS="windows", GOARCH="amd64", CGO_ENABLED="0", GOTOOLCHAIN="go1.20.14")
rc = sh(["go", "build", "-trimpath", "-ldflags", "-s -w", "-o", "vinzflash.exe", "."], env)

if rc == 0 and os.path.exists("vinzflash.exe"):
    print("BERHASIL: vinzflash.exe (%d KB) -> %s" % (os.path.getsize("vinzflash.exe") // 1024, os.path.join(here, "vinzflash.exe")))
else:
    print("GAGAL build (kode %s). Lihat error di atas, atau pakai GitHub Actions." % rc)
    sys.exit(1)
