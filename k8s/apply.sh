#!/bin/bash
set -e

echo "Building and pushing Docker images..."

docker build -t nrubiano01/pairly-server:latest -f backend/Dockerfile ./backend
docker push nrubiano01/pairly-server:latest

docker build -t nrubiano01/pairly-executor:latest -f backend/Dockerfile.executor ./backend
docker push nrubiano01/pairly-executor:latest

docker build -t nrubiano01/pairly-frontend:latest -f frontend/Dockerfile ./frontend
docker push nrubiano01/pairly-frontend:latest

echo "Images pushed successfully!"

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

echo "Restarting deployments to pick up new images..."
kubectl rollout restart deployment/websocket-server -n pairly
kubectl rollout restart deployment/executor -n pairly
kubectl rollout restart deployment/frontend -n pairly

echo "Waiting for rollouts to complete..."
kubectl rollout status deployment/websocket-server -n pairly --timeout=120s
kubectl rollout status deployment/executor -n pairly --timeout=120s
kubectl rollout status deployment/frontend -n pairly --timeout=120s

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