from pathlib import Path
from html.parser import HTMLParser
import json,hashlib,subprocess
class Text(HTMLParser):
 def __init__(self): super().__init__();self.data=[]
 def handle_data(self,s):self.data.append(s)
rows=[]
for locale in ('','pt-BR/'):
 for page in ('architecture','releases','setup'):
  p=Path('internal/docsserver/site')/locale/'docs/next'/f'{page}.html'
  h=Text();h.feed(p.read_text());t=' '.join(' '.join(h.data).split())
  if page!='setup':
   assert '77d5fd6c' in t,p
   assert '0.162.1' in t,p
   assert 'PUBLICATION HOLD' not in t and 'PUBLICAÇÃO EM HOLD' not in t,p
   assert 'v0.15.0-final-runtime-qualification.md' in p.read_text(),p
  else:assert ('local fake provider' if not locale else 'provedor falso local') in t,p
  assert '--ignore-user-config' in t,p
  rows.append({'path':str(p),'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'result':'PASS'})
Path('/var/tmp/ivoai-v015-final-doc-qualification-20261029/rendered.json').write_text(json.dumps(rows,indent=2)+'\n')
print('PASS',len(rows),'current EN/PT embedded pages')
