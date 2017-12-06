envsubst </etc/jaeger.yaml >/etc/envoy-jaeger.yaml
envoy -c /etc/envoy.json --service-cluster front-proxy --service-node `hostname`