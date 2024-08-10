## liteargs

`liteargs` is a tool which serves the same purpose as **xargs** or [**gargs**](https://github.com/brentp/gargs) - execute multiple similar commands based on the provided input parameters. The main difference is that `liteargs` state backed in the local SQLite file which provides more flexible way to control execution, ease introspection of command results and more robust re-run of the commands.

See following simple example of using `liteargs`:
```sh
$> echo 'url\nhttps://httpbin.org/status/400\nhttps://httpbin.org/status/200\nhttps://httpbin.org/status/500\nhttps://httpbin.org/get\n' > urls.csv
$> liteargs load urls.db -i urls.csv # load data to the sqlite3 db
info : successfully loaded 4 records
$> liteargs exec urls.db "curl --silent --fail '{{ .url }}'" --show --take 3 # preview commands which will be executed
curl --silent --fail 'https://httpbin.org/status/400'
curl --silent --fail 'https://httpbin.org/status/200'
curl --silent --fail 'https://httpbin.org/status/500'
$> liteargs exec urls.db "curl --silent --fail '{{ .url }}'" --parallelism 2 # run commands with given parallelism
trace: command started: curl --silent --fail 'https://httpbin.org/status/400'
trace: command started: curl --silent --fail 'https://httpbin.org/status/200'
error: command failed: curl --silent --fail 'https://httpbin.org/status/400', err=exit status 22
ok   : command succeed: curl --silent --fail 'https://httpbin.org/status/200', elapsed=985.289078ms
trace: command started: curl --silent --fail 'https://httpbin.org/status/500'
trace: command started: curl --silent --fail 'https://httpbin.org/get'
error: command failed: curl --silent --fail 'https://httpbin.org/status/500', err=exit status 22
ok   : command succeed: curl --silent --fail 'https://httpbin.org/get', elapsed=662.076923ms
info : succeed: 2, failed: 2, elapsed=1.661922228s
$> liteargs exec urls.db "curl --silent --fail '{{ .url }}'" --parallelism 1 # re-run failed commands (succeed commands will be excluded automatically)
trace: command started: curl --silent --fail 'https://httpbin.org/status/400'
error: command failed: curl --silent --fail 'https://httpbin.org/status/400', err=exit status 22
trace: command started: curl --silent --fail 'https://httpbin.org/status/500'
error: command failed: curl --silent --fail 'https://httpbin.org/status/500', err=exit status 22
info : succeed: 0, failed: 2, elapsed=1.609851067s
$> liteargs shell urls.db # inspect sqlite state if you need
→  SELECT * FROM liteargs;
URL                                SUCCEED     ATTEMPTS     LAST STDOUT                                                           LAST STDERR     LAST ATTEMPT DT     
https://httpbin.org/status/400     0           2                                                                                                  2024-08-10 23:12:54
https://httpbin.org/status/200     1           1                                                                                                  2024-08-10 23:12:35
https://httpbin.org/status/500     0           2                                                                                                  2024-08-10 23:12:55
https://httpbin.org/get            1           1            {                                                                                     2024-08-10 23:12:36
                                                              "args": {},
                                                              "headers": {
                                                                "Accept": "*/*",
                                                                "Host": "httpbin.org",
                                                                "User-Agent": "curl/7.81.0",
                                                                "X-Amzn-Trace-Id": "Root=1-66b7bba4-2815de821966fb982b22d412"
                                                              },
                                                              "origin": "78.109.76.216",                                                                            
                                                              "url": "https://httpbin.org/get"                                                                      
                                                            }
```
