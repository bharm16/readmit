#!/bin/bash
set -euo pipefail
# All secrets are generated for this lab and mounted read-only, never printed.
export LAB_STORE_PASSWORD
LAB_STORE_PASSWORD=$(cat /run/lab/keystore-password)
mkdir -p appdata
if [ ! -f appdata/keystore.jks ]; then
  keytool -importkeystore -noprompt -srckeystore /run/lab/engine.p12 -srcstoretype PKCS12 -srcstorepass:env LAB_STORE_PASSWORD -destkeystore appdata/keystore.jks -deststoretype JCEKS -deststorepass:env LAB_STORE_PASSWORD -destkeypass:env LAB_STORE_PASSWORD >/dev/null 2>&1
fi
sed -i "s/^keystore.storepass = .*/keystore.storepass = ${LAB_STORE_PASSWORD}/; s/^keystore.keypass = .*/keystore.keypass = ${LAB_STORE_PASSWORD}/" conf/mirth.properties
# The generated CA is the only authority used for fixture HTTPS calls.
if [ ! -f appdata/lab-trust.jks ]; then
  keytool -importcert -noprompt -alias lab-ca -file /run/lab/ca.pem -keystore appdata/lab-trust.jks -storepass:env LAB_STORE_PASSWORD >/dev/null 2>&1
fi
exec java @conf/default_modules.vmoptions -Xmx512m -Djava.awt.headless=true -Djavax.net.ssl.trustStore=appdata/lab-trust.jks -Djavax.net.ssl.trustStorePassword="$LAB_STORE_PASSWORD" -jar mirth-server-launcher.jar
