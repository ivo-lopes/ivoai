import {readdir, readFile} from 'node:fs/promises';
import path from 'node:path';

const root = path.resolve(import.meta.dirname, '..');
const docsRoot = path.resolve(root, '..', 'docs');
const translatedRoot = path.join(root, 'i18n', 'pt-BR', 'docusaurus-plugin-content-docs');

const markdownFiles = async (directory) => (await readdir(directory, {recursive: true}))
  .filter((entry) => /\.mdx?$/.test(entry))
  .sort();

const fences = (value) => [...value.matchAll(/```[\s\S]*?```/g)].map((match) => match[0]);
const destinations = (value) => [...value.matchAll(/\]\(([^)]+)\)/g)].map((match) => match[1]).sort();

async function validatePair(label, source, translated) {
  const english = await markdownFiles(source);
  const portuguese = await markdownFiles(translated);
  const missing = english.filter((file) => !portuguese.includes(file));
  const extra = portuguese.filter((file) => !english.includes(file));
  if (missing.length || extra.length) {
    throw new Error(`pt-BR ${label} parity failed; missing=${missing.join(',') || 'none'} extra=${extra.join(',') || 'none'}`);
  }
  for (const file of portuguese) {
    const body = await readFile(path.join(translated, file), 'utf8');
    const sourceBody = await readFile(path.join(source, file), 'utf8');
    if (!/^#\s+/m.test(body) || body.trim().length < 40) {
      throw new Error(`pt-BR ${label} translation is incomplete: ${file}`);
    }
    if (JSON.stringify(fences(body)) !== JSON.stringify(fences(sourceBody))) {
      throw new Error(`pt-BR ${label} code blocks drifted from the source document: ${file}`);
    }
    if (JSON.stringify(destinations(body)) !== JSON.stringify(destinations(sourceBody))) {
      throw new Error(`pt-BR ${label} link destinations drifted from the source document: ${file}`);
    }
  }
  return english.length;
}

const currentCount = await validatePair('current', docsRoot, path.join(translatedRoot, 'current'));
const versions = JSON.parse(await readFile(path.join(root, 'versions.json'), 'utf8'));
if (!Array.isArray(versions) || typeof versions[0] !== 'string') {
  throw new Error('documentation versions are unavailable');
}
const releaseCount = await validatePair(
  `version-${versions[0]}`,
  path.join(root, 'versioned_docs', `version-${versions[0]}`),
  path.join(translatedRoot, `version-${versions[0]}`),
);

console.log(`DOCS_I18N_PARITY=PASS current=${currentCount} release=${versions[0]}:${releaseCount}`);
