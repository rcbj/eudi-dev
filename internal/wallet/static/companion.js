// The Wallet Companion extension (github.com/sirosfoundation/wallet-companion)
// answers navigator.credentials.get() in a browser without the Digital
// Credentials API. It opens this page as
// ?request_id=...&client_id=...&request=<signed request> and waits for a
// WC_WALLET_RESPONSE message. The wallet answers as it does on /api/dc-api,
// with the verifier page's origin in place of the platform's.
(function() {
  'use strict';

  const PROTOCOL = 'openid4vp-v1-signed';
  const ACK_TIMEOUT_MS = 10000;
  const params = new URLSearchParams(window.location.search);
  const requestId = params.get('request_id');
  const status = document.getElementById('status');

  const show = (text) => { status.textContent = text; };

  // The extension's content script adds window.WalletCompanion after this page starts.
  function waitForCompanion(timeoutMs) {
    return new Promise((resolve) => {
      const started = Date.now();
      (function poll() {
        if (window.WalletCompanion) {
          resolve(window.WalletCompanion);
        } else if (Date.now() - started > timeoutMs) {
          resolve(null);
        } else {
          setTimeout(poll, 100);
        }
      })();
    });
  }

  async function offerRegistration() {
    const companion = await waitForCompanion(3000);
    if (!companion) {
      show('The Wallet Companion extension is not installed or not enabled for this page.');
      return;
    }
    const walletURL = window.location.origin + window.location.pathname;
    if (await companion.isWalletRegistered(walletURL)) {
      show('This wallet is registered with Wallet Companion as ' + walletURL + '.');
      return;
    }
    show('This wallet is not registered with Wallet Companion yet.');
    document.getElementById('register').hidden = false;
    document.getElementById('register-button').addEventListener('click', async () => {
      try {
        const result = await companion.registerWallet({
          name: 'EUDI Dev Wallet',
          url: walletURL,
          protocols: [PROTOCOL],
          description: 'eudi-dev wallet at ' + window.location.origin,
        });
        if (result && (result.success || result.alreadyRegistered)) {
          document.getElementById('register').hidden = true;
          show('This wallet is registered with Wallet Companion as ' + walletURL + '.');
        } else {
          show('Registration was not completed.');
        }
      } catch (e) {
        show('Registration failed: ' + e.message);
      }
    });
  }

  // The extension answers WC_ORIGIN_CHECK from the verifier page, so the
  // browser sets event.origin to that page's origin. The client_id query
  // parameter is the extension's claim and is not used as the origin.
  function verifierOrigin(opener) {
    return new Promise((resolve, reject) => {
      let retry = null;
      const timer = setTimeout(() => {
        clearInterval(retry);
        window.removeEventListener('message', onMessage);
        reject(new Error('The extension did not confirm the verifier origin.'));
      }, ACK_TIMEOUT_MS);
      function onMessage(event) {
        const data = event.data;
        if (event.source !== opener || !data || data.type !== 'WC_ORIGIN_ACK' || data.requestId !== requestId) {
          return;
        }
        clearTimeout(timer);
        clearInterval(retry);
        window.removeEventListener('message', onMessage);
        resolve(event.origin);
      }
      window.addEventListener('message', onMessage);
      const ask = () => opener.postMessage({ type: 'WC_ORIGIN_CHECK', requestId: requestId }, '*');
      ask();
      retry = setInterval(ask, 500);
    });
  }

  async function answer() {
    const opener = window.opener;
    if (!opener) {
      show('This page was not opened by the Wallet Companion extension, so there is nobody to answer.');
      return;
    }
    const request = params.get('request');
    if (!request) {
      show('Only signed requests (' + PROTOCOL + ') are supported, and this one has no request parameter.');
      opener.postMessage({ type: 'WC_WALLET_RESPONSE', requestId: requestId, error: 'unsupported_request' }, '*');
      return;
    }

    let origin;
    try {
      origin = await verifierOrigin(opener);
    } catch (e) {
      show(e.message);
      return;
    }
    const reply = (message) => opener.postMessage(Object.assign({ type: 'WC_WALLET_RESPONSE', requestId: requestId }, message), origin);

    show('Request from ' + origin + '. Waiting for the wallet…');
    document.getElementById('consent').hidden = false;
    let resp;
    let result = null;
    try {
      resp = await fetch('api/companion/dc-api', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          origin: origin,
          digital: { requests: [{ protocol: PROTOCOL, data: { request: request } }] },
        }),
      });
      result = await resp.json().catch(() => null);
    } catch (e) {
      document.getElementById('consent').hidden = true;
      show('The wallet could not be reached: ' + e.message);
      reply({ error: 'wallet_unreachable' });
      return;
    }
    document.getElementById('consent').hidden = true;

    if (resp.ok && result && result.protocol === PROTOCOL) {
      // OpenID4VP 1.0 Appendix A.4: a protocol error is also a result, as
      // { error } inside data, and the verifier reads it.
      reply({ response: result.data });
      show(result.data && result.data.error
        ? 'The wallet answered ' + origin + ' with the error ' + result.data.error + '.'
        : 'The presentation was sent to ' + origin + '.');
    } else {
      const reason = (result && (result.error_description || result.error)) || ('HTTP ' + resp.status);
      show('The wallet refused the request: ' + reason);
      reply({ error: (result && result.error) || 'server_error' });
      return;
    }
    setTimeout(() => window.close(), 1500);
  }

  if (requestId) {
    answer();
  } else {
    offerRegistration();
  }
})();
