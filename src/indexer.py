# Copyright 2026 Canonical
# See LICENSE file for licensing details.

"""Representation of the ubimi workload."""

import json
import os
from pathlib import Path

import ops

# Location the charm writes the rendered TOML configuration to, matching the
# rock's default service command.
CONFIG_PATH = Path("/etc/ubimi/config.toml")
PORT = 8080
RSYNC_PORT = 873


def _toml_str(value: str) -> str:
    """Return value as a quoted TOML basic string.

    JSON string escaping is a strict subset of TOML basic-string escaping, so
    ``json.dumps`` output is a valid TOML string.
    """
    return json.dumps(value)


class Indexer:
    """Represent a ubimi instance in the workload container."""

    def __init__(self, container: ops.Container):
        self.container = container

    def pebble_layer(self) -> ops.pebble.Layer:
        """Return a Pebble layer managing the ubimi service."""
        proxy_env = {
            "HTTP_PROXY": os.environ.get("JUJU_CHARM_HTTP_PROXY", ""),
            "HTTPS_PROXY": os.environ.get("JUJU_CHARM_HTTPS_PROXY", ""),
            "NO_PROXY": os.environ.get("JUJU_CHARM_NO_PROXY", ""),
        }
        return ops.pebble.Layer(
            {
                "services": {
                    "ubimi": {
                        "override": "replace",
                        "summary": "ubimi index server",
                        "command": f"/usr/bin/ubimi -config {CONFIG_PATH}",
                        "startup": "enabled",
                        "environment": proxy_env,
                    },
                },
                "checks": {
                    "up": {
                        "override": "replace",
                        "level": "alive",
                        "period": "30s",
                        "tcp": {"port": PORT},
                        "startup": "enabled",
                    },
                    "ready": {
                        "override": "replace",
                        "period": "30s",
                        "http": {"url": f"http://localhost:{PORT}/"},
                        "startup": "enabled",
                    },
                },
            }
        )

    def render_config(
        self,
        *,
        auth_url: str,
        user: str,
        password: str,
        public_url: str,
        auth_version: str = "",
        user_domain: str = "",
        project: str = "",
        project_domain: str = "",
        region: str = "",
        listen: str = ":8080",
        refresh_interval: str = "5m",
        rsync_listen: str = "",
    ) -> str:
        """Render the ubimi TOML configuration.

        Only values are transported here: defaults and validation live in the
        workload's own config loader, which rejects unknown or missing keys at
        startup.
        """
        lines = [f"listen = {_toml_str(listen)}", "", "[storage]"]
        if auth_version:
            lines.append(f"auth_version = {auth_version}")
        for key, value in (
            ("auth_url", auth_url),
            ("user", user),
            ("password", password),
            ("user_domain", user_domain),
            ("project", project),
            ("project_domain", project_domain),
            ("region", region),
            ("public_url", public_url),
        ):
            if value:
                lines.append(f"{key} = {_toml_str(value)}")

        lines += ["", "[index]", f"refresh_interval = {_toml_str(refresh_interval)}"]

        if rsync_listen:
            lines += ["", "[rsync]", f"listen = {_toml_str(rsync_listen)}"]

        return "\n".join(lines) + "\n"

    def update_config(self, content: str) -> None:
        """Write the rendered configuration into the workload container."""
        self.container.push(CONFIG_PATH, content, permissions=0o600, make_dirs=True)
