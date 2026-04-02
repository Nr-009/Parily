#!/bin/bash
set -e

echo "Starting Pairly on Kubernetes..."

echo "Creating namespace..."
kubectl apply -f k8s/namespace.yaml

echo "Applying secrets and config..."
kubectl apply -f k8s/secrets.yaml
kubectl apply -f k8s/configmap.yaml

echo "Applying infrastructure..."
kubectl apply -f k8s/postgres/
kubectl apply -f k8s/mongodb/
kubectl apply -f k8s/redis/
kubectl apply -f k8s/kafka/

echo "Waiting for databases to be ready..."
kubectl wait --for=condition=ready pod -l app=postgres -n pairly --timeout=120s
kubectl wait --for=condition=ready pod -l app=mongodb -n pairly --timeout=120s
kubectl wait --for=condition=ready pod -l app=redis -n pairly --timeout=120s
kubectl wait --for=condition=ready pod -l app=kafka -n pairly --timeout=120s

echo "Applying application..."
kubectl apply -f k8s/websocket-server/
kubectl apply -f k8s/executor/
kubectl apply -f k8s/frontend/

echo "Applying observability..."
kubectl apply -f k8s/prometheus/
kubectl apply -f k8s/grafana/
kubectl apply -f k8s/jaeger/

echo "Waiting for application to be ready..."
kubectl wait --for=condition=ready pod -l app=websocket-server -n pairly --timeout=120s
kubectl wait --for=condition=ready pod -l app=frontend -n pairly --timeout=120s

echo ""
echo "Pairly is running!"
echo ""
echo "Remember to run in separate terminals:"
echo "   Terminal 1: minikube tunnel"
echo "   Terminal 2: minikube mount ./grafana/provisioning:/grafana/provisioning"
echo ""
echo "URLs:"
echo "   Frontend:  http://localhost:5173"
echo "   Backend:   http://localhost:8080"
echo "   Grafana:   http://localhost:3000"
echo "   Jaeger:    http://localhost:16686"