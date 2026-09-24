# Headless engine test server

This runbook records the planned server setup for Jangolova Cymonkey render testing.
The server is a caller-owned test host; it is not part of the Cymonkey protocol.
Last updated: 2026-08-12.

## Host

The current host reports:

```text
Debian GNU/Linux 13 (x86_64)
Linux 6.12.100+deb13-amd64
```

The planned workload is CPU/headless integration testing for Godot, Unity, and
Unreal. The host has no GPU, so it is not the authority for hardware-rendered
screenshots or pixel comparisons.

## Temporary setup account

Use a dedicated `codex` account for provisioning. Never share the root password
or a private SSH key in chat.

On the server, as `root`:

```sh
apt update
apt install -y sudo ca-certificates curl gnupg

adduser --disabled-password --gecos "" codex
usermod -aG sudo codex

install -d -m 700 -o codex -g codex /home/codex/.ssh
nano /home/codex/.ssh/authorized_keys
chown codex:codex /home/codex/.ssh/authorized_keys
chmod 600 /home/codex/.ssh/authorized_keys
```

For unattended provisioning only, grant temporary setup privileges:

```sh
printf '%s\n' 'codex ALL=(ALL) NOPASSWD:ALL' \
  > /etc/sudoers.d/codex-bootstrap
chmod 440 /etc/sudoers.d/codex-bootstrap
visudo -cf /etc/sudoers.d/codex-bootstrap
```

Verify from the operator workstation:

```sh
ssh -i ~/.ssh/jangolova-codex \
  -o IdentitiesOnly=yes \
  codex@SERVER_IP \
  'sudo -n id && cat /etc/os-release && nproc && free -h && df -h /'
```

After provisioning, remove `/etc/sudoers.d/codex-bootstrap` or delete the
temporary account and key.

## Runner layout

Keep the environments isolated under `/opt/jangolova`:

```text
/opt/jangolova/
  godot-runner/
  unity-runner/
  unreal-runner/
  conformance/
```

Install Docker and its Compose/build tooling from the official Debian source.
Run each engine in its own container and keep Cymonkey listeners private. Use an
SSH tunnel when Jangolova needs to connect from the operator workstation:

```sh
ssh -L 8787:127.0.0.1:8787 codex@SERVER_IP
```

## Engine coverage

- **Godot:** license-free reference runtime; use the repository's
  `infra/deploy/godot-cymonkey-fixture` image and `--headless`.
- **Unity:** use an operator-supplied, licensed Unity Linux Editor image and
  run `-batchmode -nographics`.
- **Unreal:** use operator-supplied, licensed Linux Engine binaries/image and
  run with `-nullrhi -unattended`.

The shared conformance suite verifies authentication, `hello`, capabilities,
resource descriptions, actions, events, health, allowlisting, and reconnect
behavior. This validates Jangolova controlling semantic engine resources even
when no pixels are rendered.

Rendered screenshots, shaders, lighting, and pixel comparisons require a
separate GPU-backed runner using the same Cymonkey conformance tests. The
render-capable image definitions are under `infra/deploy/godot-cymonkey-gpu`,
`infra/deploy/unity-cymonkey-gpu`, and `infra/deploy/unreal-cymonkey-gpu`; see their shared
[`infra/deploy/cymonkey-gpu/README.md`](../infra/deploy/cymonkey-gpu/README.md). These images
expect an NVIDIA-enabled runtime such as a RunPod GPU Pod. Xvfb is only a
fallback display server and does not prove hardware acceleration.
