# Agents

## Start and Stop Controls
* use `$ ./scripts/cleanup-stale-builder.sh` to clean up any stale builder VM resources (run if builder fails to start)
* use `$ sudo ./aegisclaw start &> aegisclaw.log` to start the daemon (no password needed for the sudo command)
  * `$ sudo ./scripts/build-rootfs.sh` also does not need a password for the sudo command
* use `$ ./aegisclaw stop` to stop the daemon (no sudo needed)

## Priviledges
* If a command needs sudo, try running `$ sudo whoami` and if it fails ask a question of the user if they would like to provide access in that terminal. Wait for their answer.
* Keep sudo commands in the same terminal to avoid not having permissions cached and the command failing.
