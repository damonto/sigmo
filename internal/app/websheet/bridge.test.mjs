import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { setImmediate } from 'node:timers/promises';
import { runInNewContext } from 'node:vm';

const bridge = readFileSync(new URL('./bridge.js', import.meta.url), 'utf8')
  .replaceAll('{{CALLBACK_URL}}', JSON.stringify('https://sigmo.example/callback'))
  .replaceAll('{{ABSOLUTE_PATH_PROXY_PREFIX}}', JSON.stringify(''))
  .replaceAll('{{WFC_CLIENT_NAME}}', JSON.stringify('CarrierWFC'));

test('TS.43 callbacks are delivered in order and acknowledged with stable IDs', async () => {
  const requests = [];
  const notifications = [];
  const pending = [];
  const window = { parent: { postMessage(message) { notifications.push(message); } } };
  runInNewContext(bridge, {
    window,
    fetch(url, options) {
      assert.equal(options.mode, 'cors');
      requests.push(JSON.parse(options.body));
      return new Promise(resolve => pending.push(resolve));
    },
  });
  window.ODSAServiceFlow.deleteToken();
  window.ODSAServiceFlow.profileReadyWithActivationCode('LPA:1$smdp.example$test');
  window.ODSAServiceFlow.finishFlow();
  await setImmediate();
  assert.equal(requests.length, 1);
  for (const [index, event] of ['deleteToken', 'profileReadyWithActivationCode', 'finishFlow'].entries()) {
    assert.equal(requests.length, index + 1);
    assert.equal(requests[index].event, event);
    pending[index]({ ok: true });
    await setImmediate();
    assert.equal(notifications[index].submitted, true);
    assert.equal(notifications[index].callback.callbackId, requests[index].callbackId);
  }
  assert.equal(new Set(requests.map(callback => callback.callbackId)).size, 3);
});

test('failed callback delivery is reported to the parent', async () => {
  for (const fetch of [() => Promise.reject(new Error('offline')), () => Promise.resolve({ ok: false })]) {
    const notifications = [];
    const window = { parent: { postMessage(message) { notifications.push(message); } } };
    runInNewContext(bridge, { window, fetch });
    window.ODSAServiceFlow.finishFlow();
    await setImmediate();
    assert.equal(notifications.length, 1);
    assert.equal(notifications[0].submitted, false);
  }
});

test('a failed callback prevents subsequent events from overtaking it', async () => {
  const requests = [];
  const notifications = [];
  const window = { parent: { postMessage(message) { notifications.push(message); } } };
  runInNewContext(bridge, {
    window,
    fetch(url, options) {
      requests.push(JSON.parse(options.body));
      return Promise.resolve({ ok: false, status: 429 });
    },
  });
  window.ODSAServiceFlow.deleteToken();
  window.ODSAServiceFlow.finishFlow();
  await setImmediate();
  assert.equal(requests.length, 1);
  assert.equal(notifications.length, 2);
  assert.equal(notifications[0].callback.event, 'deleteToken');
  assert.equal(notifications[1].callback.event, 'finishFlow');
  assert.equal(notifications[0].submitted, false);
  assert.equal(notifications[1].submitted, false);
});

for (const controller of ['VoWiFiWebServiceFlow', 'WiFiCallingWebViewController', 'NsdsWebSheetController', 'CarrierWFC']) {
  for (const [method, resultCode] of [
    ['entitlementChanged', 'success'],
    ['CloseWebView', 'success'],
    ['phoneServicesAccountStatusChanged', 'success'],
    ['workflowCompleted', 'success'],
    ['workflowAbandoned', 'success'],
    ['dismissFlow', 'cancel'],
    ['cancelButtonClicked', 'cancel'],
    ['cancelButtonPressed', 'cancel'],
  ]) {
    test(`${controller}.${method}`, async () => {
      const callbacks = [];
      const window = { parent: { postMessage() {} } };
      runInNewContext(bridge, {
        window,
        fetch(url, options) {
          callbacks.push(JSON.parse(options.body));
          return Promise.resolve({ ok: true });
        },
      });
      window[controller][method]();
      await setImmediate();
      assert.equal(callbacks.length, 1);
      assert.equal(callbacks[0].controller, controller);
      assert.equal(callbacks[0].method, method);
      assert.equal(callbacks[0].resultCode, resultCode);
      assert.equal(callbacks[0].event, resultCode === 'success' ? method : 'dismissFlow');
    });
  }
}
