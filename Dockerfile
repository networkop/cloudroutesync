FROM busybox

WORKDIR /app
ADD cloudroutesync cloudroutesync

ENTRYPOINT ["/app/cloudroutesync"]