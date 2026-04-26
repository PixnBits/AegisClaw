# Agents



For start and stop controls:
* use `$ ./scripts/cleanup-stale-builder.sh` to clean up any stale builder VM resources (run if builder fails to start)
* use `$ sudo ./aegisclaw start &> aegisclaw.log` to start the daemon (no password needed for the sudo command)
  * `$ sudo ./scripts/build-rootfs.sh` also does not need a password for the sudo command
* use `$ ./aegisclaw stop` to stop the daemon (no sudo needed)
