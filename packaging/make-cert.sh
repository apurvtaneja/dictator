#!/bin/sh
# One-time: create a self-signed code-signing identity in the login keychain.
#
# An ad-hoc signature (codesign --sign -) has a designated requirement of just
# its cdhash, which changes on every build -- so macOS forgets the
# Accessibility grant each time. Signing with a fixed certificate gives a
# requirement of "identifier + certificate", which survives rebuilds.
set -eu

NAME="Dictator Local Signing"
KEYCHAIN="$HOME/Library/Keychains/login.keychain-db"

if security find-identity -p codesigning "$KEYCHAIN" | grep -q "\"$NAME\""; then
	echo "\"$NAME\" already exists."
	exit 0
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/cert.cfg" <<EOF
[req]
distinguished_name = dn
x509_extensions = ext
prompt = no
[dn]
CN = $NAME
[ext]
basicConstraints = critical, CA:false
keyUsage = critical, digitalSignature
extendedKeyUsage = critical, codeSigning
EOF

/usr/bin/openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -sha256 \
	-config "$tmp/cert.cfg" -keyout "$tmp/key.pem" -out "$tmp/cert.pem" 2>/dev/null
/usr/bin/openssl pkcs12 -export -inkey "$tmp/key.pem" -in "$tmp/cert.pem" \
	-name "$NAME" -passout pass:dictator -out "$tmp/id.p12"
security import "$tmp/id.p12" -k "$KEYCHAIN" -P dictator -T /usr/bin/codesign >/dev/null

echo "Created \"$NAME\". Builds are now signed with it; grant Accessibility once more after the next install."
