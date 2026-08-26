# A multi-stage build naming the same base image twice: ccu moves both lines as
# one update, or the runtime would copy artefacts out of a release it is no
# longer built from.
FROM quay.io/keycloak/keycloak:26.0.7 AS builder
ENV KC_HEALTH_ENABLED=true
ENV KC_DB=postgres
RUN /opt/keycloak/bin/kc.sh build --db postgres

FROM quay.io/keycloak/keycloak:26.0.7
ENV KC_DB=postgres
COPY --from=builder /opt/keycloak/ /opt/keycloak/
USER keycloak
WORKDIR /opt/keycloak
ENTRYPOINT ["/opt/keycloak/bin/kc.sh", "start", "--optimized"]
