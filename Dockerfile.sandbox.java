# goexec-sandbox-java — Java execution sandbox.
#
# Build:
#   docker build -f Dockerfile.sandbox.java -t goexec-sandbox-java:latest .

FROM eclipse-temurin:21-jdk

RUN useradd -ms /bin/sh sandbox
RUN mkdir /sandbox && chown sandbox:sandbox /sandbox

WORKDIR /sandbox
USER sandbox

CMD ["sh"]

