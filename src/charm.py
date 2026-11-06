#!/usr/bin/env python3
# Copyright 2026 Canonical
# See LICENSE file for licensing details.

"""Charmed Operator for the Ubuntu images index (ubimi)."""

import logging

import ops
from charms.traefik_k8s.v2.ingress import IngressPerAppRequirer
from ops.pebble import APIError, ConnectionError, ProtocolError

from indexer import PORT, RSYNC_PORT, Indexer

logger = logging.getLogger(__name__)

# Config options without a default that must be supplied by the operator.
REQUIRED_OPTIONS = ("auth-url", "user", "public-url")


class UbuntuImagesIndexerCharm(ops.CharmBase):
    """Charmed Operator for ubimi."""

    def __init__(self, framework: ops.Framework):
        super().__init__(framework)
        self._container = self.unit.get_container("ubimi")
        self._indexer = Indexer(self._container)

        framework.observe(self.on.ubimi_pebble_ready, self._on_config_changed)
        framework.observe(self.on.config_changed, self._on_config_changed)
        framework.observe(self.on.secret_changed, self._on_config_changed)
        framework.observe(self.on.update_status, self._on_update_status)

        self.unit.open_port(protocol="tcp", port=PORT)
        self.unit.open_port(protocol="tcp", port=RSYNC_PORT)

        # The index links are served relative to the request, so the ingress
        # URL is only used to expose the service; it needs no reconfiguration.
        self._ingress = IngressPerAppRequirer(
            self,
            host=f"{self.app.name}.{self.model.name}.svc.cluster.local",
            port=PORT,
            strip_prefix=True,
        )

    def _on_config_changed(self, _):
        """Render the configuration and (re)start the workload."""
        container = self._container
        if not container.can_connect():
            self.unit.status = ops.WaitingStatus("waiting for ubimi workload")
            return

        status = self._compute_status()
        if not isinstance(status, ops.ActiveStatus):
            self.unit.status = status
            return

        try:
            password = self._get_password()
            config = self._indexer.render_config(
                auth_url=self._config_str("auth-url"),
                user=self._config_str("user"),
                password=password,
                public_url=self._config_str("public-url"),
                auth_version=str(self.config.get("auth-version", 0) or ""),
                user_domain=self._config_str("user-domain"),
                project=self._config_str("project"),
                project_domain=self._config_str("project-domain"),
                region=self._config_str("region"),
                listen=self._config_str("listen", ":8080"),
                refresh_interval=self._config_str("refresh-interval", "5m"),
                rsync_listen=self._config_str("rsync-listen"),
            )
            self._indexer.update_config(config)
            container.add_layer("ubimi", self._indexer.pebble_layer(), combine=True)
            container.replan()
        except (ops.ModelError, ConnectionError, ProtocolError, APIError) as e:
            logger.error("failed to configure ubimi: %s", e)
            self.unit.status = ops.BlockedStatus(
                "Failed to configure the workload. Check `juju debug-log` for details."
            )
            return

        self.unit.status = ops.ActiveStatus()

    def _on_update_status(self, _):
        """Update status."""
        if not self._container.can_connect():
            self.unit.status = ops.WaitingStatus("waiting for ubimi workload")
            return
        self.unit.status = self._compute_status()

    def _compute_status(self) -> ops.StatusBase:
        """Resolve the unit status from the supplied configuration."""
        if not self.config.get("password"):
            return ops.BlockedStatus("storage password secret is required")
        try:
            password = self._get_password()
        except ops.ModelError as e:
            logger.error("failed to read storage password secret: %s", e)
            return ops.BlockedStatus("cannot read the storage password secret")
        if not password:
            return ops.BlockedStatus("storage password secret is empty")

        missing = [name for name in REQUIRED_OPTIONS if not self.config.get(name)]
        if missing:
            return ops.BlockedStatus(f"missing required config option(s): {', '.join(missing)}")
        return ops.ActiveStatus()

    def _config_str(self, key: str, default: str = "") -> str:
        """Return a string config value, falling back to default when unset."""
        value = self.config.get(key)
        if value is None or value == "":
            return default
        return str(value)

    def _get_password(self) -> str:
        """Read the object store password from the configured Juju secret."""
        secret_uri = self._config_str("password")
        if not secret_uri:
            return ""
        secret = self.model.get_secret(id=secret_uri, label="storage-password")
        return secret.get_content(refresh=True).get("password", "")


if __name__ == "__main__":  # pragma: nocover
    ops.main(UbuntuImagesIndexerCharm)
