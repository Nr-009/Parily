#!/bin/bash

echo "Tearing down Pairly..."
kubectl delete namespace pairly

echo "Waiting for namespace to be deleted..."
kubectl wait --for=delete namespace/pairly --timeout=120s 2>/dev/null || true

echo "Pairly torn down successfully"