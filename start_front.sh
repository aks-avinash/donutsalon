export JAEGER_AGENT_HOST=`getent hosts jaeger | awk '{ print $1 }'`
envsubst </etc/jaeger.yaml >/etc/envoy-jaeger.yaml
envoy -c /etc/envoy.json --service-cluster front-proxy --service-node `hostname`
