import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import vm from 'node:vm'
import ts from 'typescript'

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName
    this.children = []
    this.className = ''
    this.style = {}
    this.innerHTMLWrites = 0
    this._innerHTML = ''
    this._textContent = ''
  }

  set innerHTML(value) {
    this.innerHTMLWrites += 1
    this._innerHTML = String(value)
  }

  get innerHTML() {
    return this._innerHTML
  }

  set textContent(value) {
    this._textContent = String(value)
  }

  get textContent() {
    return this._textContent
  }

  addEventListener() {}

  appendChild(child) {
    this.children.push(child)
    return child
  }

  insertBefore(child, before) {
    const index = this.children.indexOf(before)
    if (index === -1) {
      this.children.push(child)
    } else {
      this.children.splice(index, 0, child)
    }
    return child
  }

  remove() {}

  replaceChildren(...children) {
    this.children = children
  }
}

const currentDirectory = path.dirname(fileURLToPath(import.meta.url))
const sourcePath = path.resolve(currentDirectory, '../src/helper/notification.ts')
const source = fs.readFileSync(sourcePath, 'utf8')
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2022,
  },
  fileName: sourcePath,
})

const moduleRecord = { exports: {} }
const document = {
  createElement(tagName) {
    return new FakeElement(tagName)
  },
}
const context = {
  Date,
  Map,
  clearTimeout,
  document,
  exports: moduleRecord.exports,
  module: moduleRecord,
  require(specifier) {
    if (specifier === '@/i18n') {
      return {
        i18n: {
          global: {
            t(content) {
              return content
            },
          },
        },
      }
    }
    throw new Error(`unexpected import: ${specifier}`)
  },
  setTimeout,
}
vm.runInNewContext(compiled.outputText, context, { filename: sourcePath })

const toast = new FakeElement('div')
moduleRecord.exports.initNotification({ value: toast })
const payload = '<img src=x onerror="globalThis.compromised=true">'
moduleRecord.exports.showNotification({
  content: payload,
  key: 'untrusted-error',
  type: 'alert-error',
  timeout: 0,
})

assert.equal(toast.children.length, 1)
const alert = toast.children[0]
const content = alert.children[0]
assert.equal(content.textContent, payload)
assert.equal(content.innerHTMLWrites, 0)
assert.equal(content.children.length, 0)

console.log('notification safety test passed')
