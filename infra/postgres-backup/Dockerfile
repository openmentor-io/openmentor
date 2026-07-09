# Backup sidecar for the openmentor postgres container (DECISIONS D2).
# Based on the same postgres image as the database so pg_dump matches the
# server major version; aws-cli ships the dumps to S3.
FROM postgres:16.14-alpine

RUN apk add --no-cache aws-cli

COPY backup.sh /usr/local/bin/backup.sh
RUN chmod 0755 /usr/local/bin/backup.sh

# Override the postgres image entrypoint - this container never runs a server
ENTRYPOINT ["/usr/local/bin/backup.sh"]
CMD ["daemon"]
