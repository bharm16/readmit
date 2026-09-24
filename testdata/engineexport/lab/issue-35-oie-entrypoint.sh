#!/bin/sh
set -eu
mkdir -p /lab
tar -xzf /opt/oie.tar.gz -C /lab
cd /lab/oie
exec ./oieserver
