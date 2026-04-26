import fs from 'node:fs';
import path from 'node:path';

const root = process.cwd();
const checkedExtensions = new Set(['.css', '.html', '.js', '.jsx', '.json', '.mjs', '.ts', '.tsx']);
const ignoredDirs = new Set(['dist', 'node_modules']);
const failures = [];

function walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (ignoredDirs.has(entry.name)) {
      continue;
    }

    const filePath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      walk(filePath);
      continue;
    }

    if (checkedExtensions.has(path.extname(entry.name))) {
      checkFile(filePath);
    }
  }
}

function checkFile(filePath) {
  const relativePath = path.relative(root, filePath);
  const content = fs.readFileSync(filePath, 'utf8');

  if (!content.endsWith('\n')) {
    failures.push(`${relativePath}: missing trailing newline`);
  }

  content.split('\n').forEach((line, index) => {
    if (/[ \t]$/.test(line)) {
      failures.push(`${relativePath}:${index + 1}: trailing whitespace`);
    }
  });
}

walk(root);

if (failures.length > 0) {
  console.error(failures.join('\n'));
  process.exit(1);
}

console.log('web format check passed');
