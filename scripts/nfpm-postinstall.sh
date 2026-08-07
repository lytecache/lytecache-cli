#!/bin/sh
# Runs after `apt install ./lytecache_*.deb` / `rpm -i lytecache_*.rpm`.
# Deliberately does NOT install or start anything -- lytecache ui is a
# per-user tool (it manages the installing user's own cache files, and a
# background service should run as that user, not root just because the
# package manager did), so auto-enabling a system service here would be
# surprising and wrong. This only prints the same next-step hint
# install.sh gives after a plain binary download.
echo "lytecache installed. To run the admin UI in the background: lytecache service install"
