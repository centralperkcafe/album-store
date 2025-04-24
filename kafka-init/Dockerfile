FROM confluentinc/cp-kafka:7.3.2

WORKDIR /scripts

COPY create-topics.sh /scripts/create-topics.sh
SHELL ["/bin/bash", "-c"]

ENTRYPOINT ["/bin/bash", "/scripts/create-topics.sh"] 