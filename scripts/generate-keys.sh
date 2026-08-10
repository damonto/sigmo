#!/bin/bash

umask 077
mkdir -p sigmo-keys
cd sigmo-keys

openssl genpkey -algorithm Ed25519 -out license-private.pem
openssl pkcs8 -topk8 -nocrypt -in license-private.pem -outform DER \
  | base64 -w 0 > license-private.pkcs8.b64
openssl pkey -in license-private.pem -pubout -outform DER \
  | tail -c 32 | base64 -w 0 > license-public.raw.b64

openssl genpkey -algorithm Ed25519 -out release-private.pem
openssl pkcs8 -topk8 -nocrypt -in release-private.pem -outform DER \
  | base64 -w 0 > release-private.pkcs8.b64
openssl pkey -in release-private.pem -pubout -outform DER \
  | tail -c 32 | base64 -w 0 > release-public.raw.b64

openssl rand -hex 32 | tr -d '\n' > telegram-webhook-secret.txt
openssl rand -base64 48 | tr -d '\n' > download-ticket-secret.txt
chmod 0600 ./*
