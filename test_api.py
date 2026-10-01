import os
import urllib.request
import json

body = {
    "query": "SELECT name, prompt, value_offer FROM company_configs WHERE company_id = 'raymont-epidataconsulting-com'",
    "company_config": {}
}

req = urllib.request.Request("http://localhost:8080/api/whatsapp/status?company_id=raymont-epidataconsulting-com")
try:
    with urllib.request.urlopen(req) as response:
        print("Status raymont:", response.read().decode())
except Exception as e:
    pass
