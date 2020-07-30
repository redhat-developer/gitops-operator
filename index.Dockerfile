FROM quay.io/operator-framework/upstream-opm-builder
LABEL operators.operatorframework.io.index.database.v1=/database/index.db
RUN /bin/opm index add --bundles quay.io/redhat-developer/gitops-backend-operator-bundle:v0.0.1 --generate index.Dockerfile
#RUN cp database/index.db /database/index.db
EXPOSE 50051
ENTRYPOINT ["/bin/opm"]
CMD ["registry", "serve", "--database", "/database/index.db"]
