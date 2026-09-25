import assert from 'node:assert/strict'
import test from 'node:test'
import { isCreateRequest, isNewTaskRequest, isSession } from './typescript/validators.js'

const preferences = { routingPolicy: 'balanced', permissionMode: 'ask' }
for (const [name, validate, value, workUnit] of [
  ['create', isCreateRequest, { projectId: 'p', checkoutRef: 'checkout', preferences, idempotencyKey: 'once' }, { taskId: 'TM-001' }],
  ['new-task', isNewTaskRequest, { sessionId: 's', preferences, idempotencyKey: 'next' }, { taskId: 'TM-001' }],
  ['session', isSession, { id: 's', taskId: 'coding-task', projectId: 'p', state: 'pending', preferences, configOptions: [], lastSequence: '0', recovery: 'none', createdAt: '2026-09-25T00:00:00Z', updatedAt: '2026-09-25T00:00:00Z', checkoutRef: 'checkout' }, { taskId: 'TM-001', bindingId: 'binding_1' }],
]) {
  test(name + ' accepts omission or an object, never explicit null', () => {
    assert.equal(validate(value), true)
    assert.equal(validate({ ...value, workUnit }), true)
    assert.equal(validate({ ...value, workUnit: null }), false)
  })
}
