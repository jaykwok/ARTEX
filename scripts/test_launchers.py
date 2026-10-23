"""Portable launcher regressions; no real ARTEX server is started."""
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import time
import unittest
import zipfile


ROOT = Path(__file__).resolve().parents[1]
SHELL_SCRIPTS = [*ROOT.glob('*.sh'), *ROOT.joinpath('scripts').glob('*.sh')]


class ScriptFormatTests(unittest.TestCase):
    def test_unix_scripts_are_lf_without_bom(self):
        for path in SHELL_SCRIPTS:
            with self.subTest(script=path.name):
                data = path.read_bytes()
                self.assertNotIn(b'\r', data)
                self.assertFalse(data.startswith(b'\xef\xbb\xbf'))

    def test_windows_launcher_is_ascii_crlf(self):
        data = ROOT.joinpath('start.bat').read_bytes()
        data.decode('ascii')
        self.assertNotIn(b'\n', data.replace(b'\r\n', b''))


@unittest.skipUnless(os.name == 'nt', 'Windows CMD required')
class WindowsLauncherTests(unittest.TestCase):
    def test_missing_binary_across_code_pages(self):
        for code_page in (437, 936, 65001):
            with self.subTest(code_page=code_page), tempfile.TemporaryDirectory(prefix='artex launcher ') as folder:
                shutil.copyfile(ROOT / 'start.bat', Path(folder) / 'start.bat')
                result = subprocess.run(
                    [os.environ['COMSPEC'], '/d', '/c', f'chcp {code_page} >nul & start.bat'],
                    cwd=folder, capture_output=True, timeout=5,
                    creationflags=subprocess.CREATE_NO_WINDOW,
                )
                self.assertEqual(result.returncode, 1)
                self.assertEqual(result.stdout, b'')
                self.assertEqual(result.stderr.strip(), b'[artex] Executable not found: artex.exe')
                self.assertEqual([p.name for p in Path(folder).iterdir()], ['start.bat'])


@unittest.skipIf(os.name == 'nt', 'POSIX shells and signals required')
class UnixLauncherTests(unittest.TestCase):
    def test_shell_syntax(self):
        for path in SHELL_SCRIPTS:
            shell = 'sh' if path.name == 'start.sh' else 'bash'
            with self.subTest(script=path.name):
                subprocess.run([shell, '-n', str(path)], check=True, capture_output=True)

    def launch(self, folder, shell, backend):
        target = Path(folder)
        shutil.copyfile(ROOT / 'start.sh', target / 'start.sh')
        stub = target / 'artex'
        stub.write_text('#!/bin/sh\n' + backend, encoding='utf-8')
        stub.chmod(0o755)
        return subprocess.Popen(
            [shell, str(target / 'start.sh'), '-data', 'test space'],
            cwd=ROOT, env={**os.environ, 'LC_ALL': 'C'},
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding='utf-8', start_new_session=True,
        )

    def finish(self, proc):
        if proc.poll() is None:
            os.killpg(proc.pid, signal.SIGKILL)
        proc.communicate(timeout=5)

    def test_update_crash_and_normal_exit(self):
        backend = '''
[ "$#" -eq 2 ] && [ "$1" = -data ] && [ "$2" = 'test space' ] || exit 88
attempt=0
[ ! -f attempts ] || attempt=$(cat attempts)
attempt=$((attempt + 1))
printf '%s' "$attempt" > attempts
case "$attempt" in 1) exit 75;; 2) exit 9;; *) exit 0;; esac
'''
        for shell in ('sh', 'bash'):
            with self.subTest(shell=shell), tempfile.TemporaryDirectory(prefix='artex launcher ') as folder:
                proc = self.launch(folder, shell, backend)
                try:
                    stdout, stderr = proc.communicate(timeout=8)
                    self.assertEqual(proc.returncode, 0, stderr)
                    self.assertEqual(Path(folder, 'attempts').read_text(), '3')
                    self.assertIn('请求重启', stdout)
                    self.assertIn('正常退出', stdout)
                    self.assertIn('code=9', stderr)
                    self.assertEqual({p.name for p in Path(folder).iterdir()}, {'start.sh', 'artex', 'attempts'})
                finally:
                    self.finish(proc)

    def test_stop_during_retry_does_not_restart(self):
        backend = 'echo attempt >> attempts\nexit 9\n'
        for shell in ('sh', 'bash'):
            with self.subTest(shell=shell), tempfile.TemporaryDirectory(prefix='artex launcher ') as folder:
                proc = self.launch(folder, shell, backend)
                try:
                    # Let the retry backoff grow, so a blocking sleep is observable.
                    deadline = time.monotonic() + 8
                    while time.monotonic() < deadline:
                        attempts = Path(folder, 'attempts')
                        if attempts.exists() and len(attempts.read_text().splitlines()) >= 3:
                            break
                        time.sleep(0.02)
                    else:
                        self.fail('Launcher did not reach the third attempt')
                    time.sleep(0.1)
                    proc.terminate()
                    proc.communicate(timeout=2)
                    self.assertEqual(proc.returncode, 0)
                    self.assertEqual(len(attempts.read_text().splitlines()), 3)
                finally:
                    self.finish(proc)

    def test_stop_is_forwarded_to_backend(self):
        backend = '''
trap 'echo stopped > stopped; exit 0' TERM
echo ready > ready
while :; do sleep 0.1; done
'''
        for shell in ('sh', 'bash'):
            with self.subTest(shell=shell), tempfile.TemporaryDirectory(prefix='artex launcher ') as folder:
                proc = self.launch(folder, shell, backend)
                try:
                    deadline = time.monotonic() + 5
                    while not Path(folder, 'ready').exists():
                        if time.monotonic() > deadline:
                            self.fail('Backend did not start')
                        time.sleep(0.02)
                    proc.terminate()
                    proc.communicate(timeout=3)
                    self.assertEqual(proc.returncode, 0)
                    self.assertTrue(Path(folder, 'stopped').exists())
                finally:
                    self.finish(proc)

    @unittest.skipUnless(shutil.which('zip'), 'zip required')
    def test_release_archive_script_formats(self):
        # Exercise the real packaging path with a fake compiler and CRLF inputs.
        with tempfile.TemporaryDirectory(prefix='artex package ') as folder:
            root = Path(folder)
            for directory in ('scripts', 'skills', 'server/webui/dist', 'bin'):
                root.joinpath(directory).mkdir(parents=True)
            for name in ('build.sh', 'config.example.json', 'scripts/version.sh'):
                shutil.copyfile(ROOT / name, root / name)
            for name in ('start.sh', 'reset-password.sh', 'start.bat'):
                data = (ROOT / name).read_bytes().replace(b'\r\n', b'\n')
                (root / name).write_bytes(data.replace(b'\n', b'\r\n'))
            compiler = root / 'bin/go'
            compiler.write_text('''#!/bin/sh
while [ "$#" -gt 0 ]; do
    if [ "$1" = -o ]; then
        printf 'stub binary\\n' > "$2"
        exit 0
    fi
    shift
done
exit 1
''', encoding='utf-8')
            compiler.chmod(0o755)
            result = subprocess.run(
                ['bash', 'build.sh', '--release'], cwd=root,
                env={**os.environ, 'PATH': str(root / 'bin') + os.pathsep + os.environ['PATH'],
                     'ARTEX_SKIP_FRONTEND': '1', 'ARTEX_BUILD_VERSION': 'test', 'ARTEX_COMPRESS': 'off',
                     'ARTEX_TARGETS': 'linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64'},
                capture_output=True, text=True, timeout=20,
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            archives = list((root / 'dist').glob('*.zip'))
            self.assertEqual(len(archives), 5)
            for path in archives:
                with self.subTest(archive=path.name), zipfile.ZipFile(path) as archive:
                    prefix = path.stem + '/'
                    if 'windows' in path.name:
                        data = archive.read(prefix + 'start.bat')
                        data.decode('ascii')
                        self.assertNotIn(b'\n', data.replace(b'\r\n', b''))
                    else:
                        for name in ('start.sh', 'reset-password.sh'):
                            info = archive.getinfo(prefix + name)
                            self.assertNotIn(b'\r', archive.read(info))
                            self.assertTrue((info.external_attr >> 16) & 0o111)


if __name__ == '__main__':
    unittest.main()
