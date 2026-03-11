# MySQL (MariaDB) - CDC source database with binlog enabled
# Build: docker build -t db-stream-mysql -f deployments/mysql.Dockerfile .
# Run: see deployments/README.md

FROM mariadb:10.11

ENV MYSQL_DATABASE=employee \
    MYSQL_USER=db_stream \
    MYSQL_PASSWORD=db_stream_password \
    MYSQL_ROOT_PASSWORD=root_password

# Enable row-based binary logging required for Debezium CDC.
# binlog_row_metadata=FULL ensures column metadata is included in every event.
CMD ["--server-id=1", "--log-bin=mysql-bin", "--binlog-format=ROW", "--binlog-row-metadata=FULL"]

EXPOSE 3306
