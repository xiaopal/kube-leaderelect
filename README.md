# kube-leaderelect

# build/test
```
CGO_ENABLED=0 GOOS=linux go build -o bin/kube-leaderelect -ldflags '-s -w' cmd/*.go
bin/kube-leaderelect -h
bin/kube-leaderelect --leader-elect=endpoints/sshd --endpoint-port=22 -- /usr/sbin/sshd -D

```

# docker image
```
docker run -it --rm -v /root:/root xiaopal/kube-leaderelect --leader-elect=endpoints/sshd -- bash
```