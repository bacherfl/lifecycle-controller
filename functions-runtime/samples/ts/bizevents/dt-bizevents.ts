import * as https from "https://deno.land/std@0.172.0/node/https.ts";
import * as crypto from "https://deno.land/std@0.172.0/node/crypto.ts";

const timeoutSeconds = 60
const scopes = "storage:events:read storage:events:write environment:roles:viewer"
const dtCredsStr = Deno.env.get("SECURE_DATA")

let client_id
let client_secret
let tenant


if (dtCredsStr != undefined) {
    dtCreds = JSON.parse(dtCredsStr);
    client_id = dtCreds.clientID
    client_secret = dtCreds.clientSecret
    tenant = dtCreds.Tenant
} else {
    client_id = Deno.env.get("CLIENT_ID")
    client_secret = Deno.env.get("CLIENT_SECRET")
    tenant = Deno.env.get("TENANT")
}

async function login() {
    const options = {
        hostname: 'sso-dev.dynatracelabs.com',
        port: 443,
        path: '/sso/oauth2/token',
        method: 'POST',
        headers: {
            'Content-Type': 'application/x-www-form-urlencoded',
        }
    };
    const data = new URLSearchParams({
        grant_type: 'client_credentials',
        client_id: client_id,
        client_secret: client_secret,
        scope: scopes
    });
    return new Promise((resolve, reject) => {
        let response = ""
        const req = https.request(options, (res) => {
            // TODO:  403 permissions error, 400 wrong format, 401 unauth
            if(res.statusCode < 200 || res.statusCode >= 400) {
                reject(JSON.parse(`{ "error_code": ${res.statusCode} }`))
            }
            res.on('data', (d) => {
                response += d
            });
            res.on('end', () => {
                resolve(JSON.parse(response))
            })
        });
        req.on('error', (e) => {
            reject(e);
        });
        req.write(data.toString());
        req.end();
    })
}

async function postBizEvent(bearer, tenant, _id) {
    const bizEvent = {
        specversion: '1.0',
        source: 'keptn-lifecycle-controller',
        id: _id,
        type: 'reliability.guardian.triggered',
        data: {
            rndvalue: Math.random(),
            stage: "namespace",
            appname: "appname",
            appversion: "appversion",
        },
    };

    console.log(bizEvent.id)

    const options = {
        hostname: `${tenant}.dev.apps.dynatracelabs.com`,
        port: 443,
        path: '/platform/classic/environment-api/v2/bizevents/ingest',
        method: 'POST',
        headers: {
            'Content-Type': 'application/cloudevent+json',
            'Authorization': `Bearer ${bearer}`,
        }
    };
    return new Promise((resolve, reject) => {
        let response = ""
        const req = https.request(options, (res) => {
            // TODO: 403 permissions error, 400 wrong format, 401 unauth
            if(res.statusCode < 200 || res.statusCode >= 400) {
                reject(JSON.parse(`{ "error_code": ${res.statusCode} }`))
            }
            res.on('data', (d) => {
                response += d
            });
            res.on('end', () => {
                resolve(JSON.parse(response))
            })
        });
        req.on('error', (e) => {
            reject(e);
        });
        req.write(JSON.stringify(bizEvent));
        req.end();
    })
}

async function runDQL(bearer, tenant, dql) {
    const now = new Date(Date.now());
    const sTime = new Date(now);
    sTime.setMinutes (now.getMinutes() - 5);
    now.setMinutes(now.getMinutes() + 5);
    const q = new URLSearchParams({
        query: dql,
        "default-scan-limit-gbytes": -1,
        "default-timeframe-start": sTime.toISOString(),
        "default-timeframe-end": now.toISOString(),
    })

    const options = {
        hostname: `${tenant}.dev.apps.dynatracelabs.com`,
        port: 443,
        path: '/platform/storage/query/v0.7/query:execute?' + q.toString(),
        method: 'POST',
        headers: {
            'Dt-Tenant': tenant,
            'Content-Type': 'application/cloudevent+json',
            'Authorization': `Bearer ${bearer}`,
        }
    };
    return new Promise((resolve, reject) => {
        let response = ""
        const req = https.request(options, (res) => {
            // TODO: 403 permissions error, 400 wrong format, 401 unauth
            if(res.statusCode < 200 || res.statusCode >= 400) {
                reject(JSON.parse(`{ "error_code": ${res.statusCode} }`))
            }
            res.on('data', (d) => {
                response += d
            });
            res.on('end', () => {
                resolve(JSON.parse(response))
            })
        });
        req.on('error', (e) => {
            reject(e);
        });
        //req.write();
        req.end();
    })
}

async function readDQLResults(bearer, tenant, token) {
    const q = new URLSearchParams({
        "request-token": token
    })
    const options = {
        hostname: `${tenant}.dev.apps.dynatracelabs.com`,
        port: 443,
        path: '/platform/storage/query/v0.7/query:poll?' + q.toString(),
        method: 'GET',
        headers: {
            'Content-Type': 'application/cloudevent+json',
            'Authorization': `Bearer ${bearer}`,
        }
    };
    return new Promise((resolve, reject) => {
        let response = ""
        const req = https.request(options, (res) => {
            // TODO: 403 permissions error, 400 wrong format, 401 unauth
            if(res.statusCode < 200 || res.statusCode >= 400) {
                reject(JSON.parse(`{ "error_code": ${res.statusCode} }`))
            }
            res.on('data', (d) => {
                response += d
            });
            res.on('end', () => {
                resolve(JSON.parse(response))
            })
        });
        req.on('error', (e) => {
            reject(e);
        });
        req.end();
    })
}


async function getIngestedBizEvent(bearer, tenant, _id) {
    let found = false
    let r;
    while(!found) {
        try {
            const w = await runDQL(bearer, tenant, `fetch bizevents | filter event.id == "${_id}" | limit 1`);
            r = await readDQLResults(bearer, tenant, w['requestToken']);
            found = r['result']['records'].length > 0;
            if(found) {
                continue;
            }
            await sleep(1000)
        } catch (e) {
            console.error("Errow while reading DQL", e)
        }
    }
    return r
}

async function getResponseBizEvent(bearer, tenant, _id) {
    let found = false
    let r;
    while(!found) {
        try {
            const w = await runDQL(bearer, tenant, `fetch bizevents | filter triggeredID == "${_id}" | limit 1`);
            r = await readDQLResults(bearer, tenant, w['requestToken']);
            found = r['result']['records'].length > 0;
            if(found) {
                continue;
            }
            await sleep(1000)
        } catch (e) {
            console.error("Errow while reading DQL", e)
        }
    }
    return r
}

function sleep(ms) {
    return new Promise((resolve) => {
        setTimeout(resolve, ms);
    });
}

// main
function timeout() {
    console.error("Timed out, bye")
    process.exit(1)
}
// we give the script 10s to finish
const t = setTimeout(timeout, timeoutSeconds * 1000);
// don't hang on the strong ref on t to be cleaned
Deno.unrefTimer(t)

login().then(v => {
    const bearer = v['access_token']
    const _id = crypto.randomUUID().toString()
    postBizEvent(bearer, tenant, _id).then(async w => {
        console.log("BizEvent Posted")
        // let the data propagate and avoid eventual consistency issues
        //await sleep(10000);
        let ingestedEvent = await getIngestedBizEvent(bearer, tenant, _id)
        console.log("Ingested Event:")
        console.log(ingestedEvent['result']['records'])

        let finishedEvent = await getResponseBizEvent(bearer, tenant, _id)
        console.log("Response Event:")
        console.log(finishedEvent['result']['records'])
        // wait for the release.guardian.finished event

    }).catch(e => {
        console.error("Error posting event", e)
    });

}).catch(e => {
    console.error("Error in login", e)
})