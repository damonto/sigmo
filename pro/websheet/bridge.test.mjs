import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { runInNewContext } from 'node:vm';

const bridge = readFileSync(new URL('./bridge.js', import.meta.url), 'utf8')
  .replaceAll('{{CALLBACK_URL}}', JSON.stringify('https://sigmo.example/callback'))
  .replaceAll('{{ABSOLUTE_PATH_PROXY_PREFIX}}', JSON.stringify(''))
  .replaceAll('{{WFC_CLIENT_NAME}}', JSON.stringify('CarrierWFC'));

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
    test(`${controller}.${method}`, () => {
      const callbacks = [];
      const window = { parent: { postMessage() {} } };
      runInNewContext(bridge, {
        window,
        fetch(url, options) {
          callbacks.push(JSON.parse(options.body));
          return Promise.resolve();
        },
      });
      window[controller][method]();
      assert.equal(callbacks.length, 1);
      assert.equal(callbacks[0].controller, controller);
      assert.equal(callbacks[0].method, method);
      assert.equal(callbacks[0].resultCode, resultCode);
      assert.equal(callbacks[0].event, resultCode === 'success' ? method : 'dismissFlow');
    });
  }
}
