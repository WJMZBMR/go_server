./bin/startserver n01
./bin/startserver n02
./bin/startserver n03
./bin/startserver n04
./bin/startserver n05
bin/client.py 'kv/insert?key=hello&value=world'
bin/client.py 'kv/insert?key=hello1&value=world'
bin/client.py 'kv/insert?key=hello2&value=world'
bin/client.py 'kv/insert?key=hello3&value=world'
bin/client.py 'kv/insert?key=hello4&value=world'
bin/client.py 'kvman/countkey'
./bin/stopserver n01
./bin/stopserver n02
./bin/stopserver n03
./bin/stopserver n04
./bin/stopserver n05
