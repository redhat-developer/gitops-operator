docker build -t quay.io/shbose/gitops-backend-operator:v0.0.3 .
docker push quay.io/shbose/gitops-backend-operator:v0.0.3
operator-sdk bundle create quay.io/shbose/gitops-backend-operator-bundle:v0.0.3
docker push quay.io/shbose/gitops-backend-operator-bundle:v0.0.3
opm index add --bundles quay.io/shbose/gitops-backend-operator-bundle:v0.0.3 --tag quay.io/shbose/gitops-backend-operator-index:v0.0.3 --build-tool=docker
docker push quay.io/shbose/gitops-backend-operator-index:v0.0.3
