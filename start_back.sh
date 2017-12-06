envsubst </etc/jaeger.yaml >/etc/envoy-jaeger.yaml
donutbin --service_hostport="front-envoy:80" &
envoy -c /etc/envoy.json --service-cluster donutsalon-${SERVICE_NAME} --service-node `hostname`
