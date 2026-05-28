(function(){
  const callbackURL = {{CALLBACK_URL}};
  const absolutePathProxyPrefix = {{ABSOLUTE_PATH_PROXY_PREFIX}};
  const odsaStorageKey = "ts43-go.odsa.callbacks";
  const vowifiStorageKey = "ims-client.vowifi.callbacks";

  function rewriteCarrierPath(value) {
    if (!absolutePathProxyPrefix || typeof value !== "string") return value;
    if (value.charAt(0) !== "/" || value.indexOf("//") === 0) return value;
    if (value.indexOf("/api/v1/websheets/") === 0) return value;
    return absolutePathProxyPrefix + value;
  }

  function installRequestRewriter() {
    try {
      const originalOpen = window.XMLHttpRequest && window.XMLHttpRequest.prototype && window.XMLHttpRequest.prototype.open;
      if (originalOpen) {
        window.XMLHttpRequest.prototype.open = function(method, url) {
          const args = Array.prototype.slice.call(arguments);
          args[1] = rewriteCarrierPath(url);
          return originalOpen.apply(this, args);
        };
      }
    } catch (_) {}

    try {
      const originalFetch = window.fetch;
      if (originalFetch) {
        window.fetch = function(input, init) {
          if (typeof input === "string") {
            input = rewriteCarrierPath(input);
          }
          return originalFetch.call(this, input, init);
        };
      }
    } catch (_) {}
  }

  installRequestRewriter();

  function notifyParent(payload) {
    try {
      window.parent.postMessage({type:"sigmo-websheet-callback", callback: payload}, "*");
    } catch (_) {}
  }

  function post(payload) {
    if (!payload.at) payload.at = new Date().toISOString();
    payload.href = window.location && window.location.href ? window.location.href : "";
    if (payload.source === "odsa") {
      appendStorage(odsaStorageKey, payload);
      try {
        window.dispatchEvent(new CustomEvent("ts43-odsa-callback", {detail: payload}));
      } catch (_) {}
    }
    if (payload.source === "vowifi") {
      appendStorage(vowifiStorageKey, payload);
      try {
        window.dispatchEvent(new CustomEvent("vowifi-callback", {detail: payload}));
      } catch (_) {}
    }
    try {
      fetch(callbackURL, {method: "POST", mode: "no-cors", headers: {"Content-Type": "text/plain;charset=UTF-8"}, body: JSON.stringify(payload), credentials: "omit"})
        .then(function(){ notifyParent(payload); })
        .catch(function(){ notifyParent(payload); });
    } catch (_) {
      notifyParent(payload);
    }
  }

  function appendStorage(key, payload) {
    try {
      const existing = JSON.parse(window.localStorage.getItem(key) || "[]");
      existing.push(payload);
      window.localStorage.setItem(key, JSON.stringify(existing.slice(-20)));
    } catch (_) {}
  }

  function storedCallbacks(key) {
    try {
      return JSON.parse(window.localStorage.getItem(key) || "[]");
    } catch (_) {
      return [];
    }
  }

  const flow = window.ODSAServiceFlow || {};
  flow.profileReadyWithActivationCode = function(activationCode, iccid, imei) { post({source:"odsa", controller:"ODSAServiceFlow", method:"profileReadyWithActivationCode", event:"profileReadyWithActivationCode", activationCode: activationCode || "", iccid: iccid || "", imei: imei || ""}); };
  flow.profileReadyWithDefaultSmdp = function(defaultSmdpAddress, iccid, imei) { post({source:"odsa", controller:"ODSAServiceFlow", method:"profileReadyWithDefaultSmdp", event:"profileReadyWithDefaultSmdp", defaultSmdpAddress: defaultSmdpAddress || "", iccid: iccid || "", imei: imei || ""}); };
  flow.profileReadyWithDefaultSMDP = flow.profileReadyWithDefaultSmdp;
  flow.selectionCompleted = function(iccid, imei) { post({source:"odsa", controller:"ODSAServiceFlow", method:"selectionCompleted", event:"selectionCompleted", iccid: iccid || "", imei: imei || ""}); };
  flow.finishFlow = function(nextAction) { post({source:"odsa", controller:"ODSAServiceFlow", method:"finishFlow", event:"finishFlow", nextAction: nextAction || ""}); };
  flow.dismissFlow = function() { post({source:"odsa", controller:"ODSAServiceFlow", method:"dismissFlow", event:"dismissFlow"}); };
  flow.deleteToken = function() { post({source:"odsa", controller:"ODSAServiceFlow", method:"deleteToken", event:"deleteToken"}); };
  flow.checkProfileServiceStatus = function() { post({source:"odsa", controller:"ODSAServiceFlow", method:"checkProfileServiceStatus", event:"checkProfileServiceStatus"}); };
  flow.deleteProfileInUse = function(iccid) { post({source:"odsa", controller:"ODSAServiceFlow", method:"deleteProfileInUse", event:"deleteProfileInUse", iccid: iccid || ""}); };
  window.ODSAServiceFlow = flow;
  window.ts43ODSAServiceFlow = Object.freeze({
    callbacks: function() {
      return storedCallbacks(odsaStorageKey);
    }
  });

  function vowifiEvent(method) {
    switch (method) {
    case "entitlementChanged":
      return "entitlementChanged";
    case "dismissFlow":
    case "cancelButtonClicked":
    case "CloseWebView":
    case "closeWebView":
    case "onCloseWebView":
      return "dismissFlow";
    default:
      return method;
    }
  }

  function vowifiResult(event) {
    switch (event) {
    case "entitlementChanged":
      return "success";
    case "dismissFlow":
      return "cancel";
    default:
      return "";
    }
  }

  function vowifiMethod(controller, method) {
    return function() {
      const event = vowifiEvent(method);
      const payload = {
        source: "vowifi",
        controller: controller,
        method: method,
        event: event,
        args: Array.prototype.slice.call(arguments)
      };
      const resultCode = vowifiResult(event);
      if (resultCode) payload.resultCode = resultCode;
      post(payload);
    };
  }

  function installVowifiController(name, methods) {
    const target = window[name] || {};
    for (let i = 0; i < methods.length; i++) {
      const method = methods[i];
      if (typeof target[method] !== "function") {
        target[method] = vowifiMethod(name, method);
      }
    }
    window[name] = target;
    return target;
  }

  const voWiFiWebServiceFlow = installVowifiController("VoWiFiWebServiceFlow", ["entitlementChanged", "dismissFlow"]);
  const wifiCallingWebViewController = installVowifiController("WiFiCallingWebViewController", ["cancelButtonClicked", "cancelButtonPressed", "phoneServicesAccountStatusChanged", "CloseWebView", "closeWebView", "onCloseWebView"]);
  const nsdsWebSheetController = installVowifiController("NsdsWebSheetController", ["entitlementChanged", "dismissFlow", "cancelButtonClicked", "cancelButtonPressed", "phoneServicesAccountStatusChanged", "CloseWebView", "closeWebView", "onCloseWebView"]);
  window.vowifiCallback = Object.freeze({
    done: voWiFiWebServiceFlow.entitlementChanged,
    dismiss: voWiFiWebServiceFlow.dismissFlow,
    controllers: Object.freeze({
      VoWiFiWebServiceFlow: voWiFiWebServiceFlow,
      WiFiCallingWebViewController: wifiCallingWebViewController,
      NsdsWebSheetController: nsdsWebSheetController
    }),
    callbacks: function() {
      return storedCallbacks(vowifiStorageKey);
    }
  });
})();
