"use strict";
// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
Object.defineProperty(exports, "__esModule", { value: true });
exports.GcpDetectorSync = void 0;
const semantic_conventions_1 = require("@opentelemetry/semantic-conventions");
const resources_1 = require("@opentelemetry/resources");
const metadata = require("gcp-metadata");
const faas = require("./faas");
const gae = require("./gae");
const gce = require("./gce");
const gke = require("./gke");
const ATTRIBUTE_NAMES = [
    semantic_conventions_1.SEMRESATTRS_CLOUD_PLATFORM,
    semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE,
    semantic_conventions_1.SEMRESATTRS_CLOUD_REGION,
    semantic_conventions_1.SEMRESATTRS_K8S_CLUSTER_NAME,
    semantic_conventions_1.SEMRESATTRS_HOST_TYPE,
    semantic_conventions_1.SEMRESATTRS_HOST_ID,
    semantic_conventions_1.SEMRESATTRS_HOST_NAME,
    semantic_conventions_1.SEMRESATTRS_CLOUD_PROVIDER,
    semantic_conventions_1.SEMRESATTRS_CLOUD_ACCOUNT_ID,
    semantic_conventions_1.SEMRESATTRS_FAAS_NAME,
    semantic_conventions_1.SEMRESATTRS_FAAS_VERSION,
    semantic_conventions_1.SEMRESATTRS_FAAS_INSTANCE,
];
async function detect() {
    if (!(await metadata.isAvailable())) {
        return (0, resources_1.emptyResource)();
    }
    // Note the order of these if checks is significant with more specific resources coming
    // first. E.g. Cloud Functions gen2 are executed in Cloud Run so it must be checked first.
    if (await gke.onGke()) {
        return await gkeResource();
    }
    else if (await faas.onCloudFunctions()) {
        return await cloudFunctionsResource();
    }
    else if (await faas.onCloudRun()) {
        return await cloudRunResource();
    }
    else if (await gae.onAppEngine()) {
        return await gaeResource();
    }
    else if (await gce.onGce()) {
        return await gceResource();
    }
    return (0, resources_1.emptyResource)();
}
async function gkeResource() {
    const [zoneOrRegion, k8sClusterName, hostId] = await Promise.all([
        gke.availabilityZoneOrRegion(),
        gke.clusterName(),
        gke.hostId(),
    ]);
    return await makeResource({
        [semantic_conventions_1.SEMRESATTRS_CLOUD_PLATFORM]: semantic_conventions_1.CLOUDPLATFORMVALUES_GCP_KUBERNETES_ENGINE,
        [zoneOrRegion.type === 'zone'
            ? semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE
            : semantic_conventions_1.SEMRESATTRS_CLOUD_REGION]: zoneOrRegion.value,
        [semantic_conventions_1.SEMRESATTRS_K8S_CLUSTER_NAME]: k8sClusterName,
        [semantic_conventions_1.SEMRESATTRS_HOST_ID]: hostId,
    });
}
async function cloudRunResource() {
    const [faasName, faasVersion, faasInstance, faasCloudRegion] = await Promise.all([
        faas.faasName(),
        faas.faasVersion(),
        faas.faasInstance(),
        faas.faasCloudRegion(),
    ]);
    return await makeResource({
        [semantic_conventions_1.SEMRESATTRS_CLOUD_PLATFORM]: semantic_conventions_1.CLOUDPLATFORMVALUES_GCP_CLOUD_RUN,
        [semantic_conventions_1.SEMRESATTRS_FAAS_NAME]: faasName,
        [semantic_conventions_1.SEMRESATTRS_FAAS_VERSION]: faasVersion,
        [semantic_conventions_1.SEMRESATTRS_FAAS_INSTANCE]: faasInstance,
        [semantic_conventions_1.SEMRESATTRS_CLOUD_REGION]: faasCloudRegion,
    });
}
async function cloudFunctionsResource() {
    const [faasName, faasVersion, faasInstance, faasCloudRegion] = await Promise.all([
        faas.faasName(),
        faas.faasVersion(),
        faas.faasInstance(),
        faas.faasCloudRegion(),
    ]);
    return await makeResource({
        [semantic_conventions_1.SEMRESATTRS_CLOUD_PLATFORM]: semantic_conventions_1.CLOUDPLATFORMVALUES_GCP_CLOUD_FUNCTIONS,
        [semantic_conventions_1.SEMRESATTRS_FAAS_NAME]: faasName,
        [semantic_conventions_1.SEMRESATTRS_FAAS_VERSION]: faasVersion,
        [semantic_conventions_1.SEMRESATTRS_FAAS_INSTANCE]: faasInstance,
        [semantic_conventions_1.SEMRESATTRS_CLOUD_REGION]: faasCloudRegion,
    });
}
async function gaeResource() {
    let zone, region;
    if (await gae.onAppEngineStandard()) {
        [zone, region] = await Promise.all([
            gae.standardAvailabilityZone(),
            gae.standardCloudRegion(),
        ]);
    }
    else {
        ({ zone, region } = await gce.availabilityZoneAndRegion());
    }
    const [faasName, faasVersion, faasInstance] = await Promise.all([
        gae.serviceName(),
        gae.serviceVersion(),
        gae.serviceInstance(),
    ]);
    return await makeResource({
        [semantic_conventions_1.SEMRESATTRS_CLOUD_PLATFORM]: semantic_conventions_1.CLOUDPLATFORMVALUES_GCP_APP_ENGINE,
        [semantic_conventions_1.SEMRESATTRS_FAAS_NAME]: faasName,
        [semantic_conventions_1.SEMRESATTRS_FAAS_VERSION]: faasVersion,
        [semantic_conventions_1.SEMRESATTRS_FAAS_INSTANCE]: faasInstance,
        [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE]: zone,
        [semantic_conventions_1.SEMRESATTRS_CLOUD_REGION]: region,
    });
}
async function gceResource() {
    const [zoneAndRegion, hostType, hostId, hostName] = await Promise.all([
        gce.availabilityZoneAndRegion(),
        gce.hostType(),
        gce.hostId(),
        gce.hostName(),
    ]);
    return await makeResource({
        [semantic_conventions_1.SEMRESATTRS_CLOUD_PLATFORM]: semantic_conventions_1.CLOUDPLATFORMVALUES_GCP_COMPUTE_ENGINE,
        [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE]: zoneAndRegion.zone,
        [semantic_conventions_1.SEMRESATTRS_CLOUD_REGION]: zoneAndRegion.region,
        [semantic_conventions_1.SEMRESATTRS_HOST_TYPE]: hostType,
        [semantic_conventions_1.SEMRESATTRS_HOST_ID]: hostId,
        [semantic_conventions_1.SEMRESATTRS_HOST_NAME]: hostName,
    });
}
async function makeResource(attrs) {
    const project = await metadata.project('project-id');
    return (0, resources_1.resourceFromAttributes)({
        [semantic_conventions_1.SEMRESATTRS_CLOUD_PROVIDER]: semantic_conventions_1.CLOUDPROVIDERVALUES_GCP,
        [semantic_conventions_1.SEMRESATTRS_CLOUD_ACCOUNT_ID]: project,
        ...attrs,
    });
}
/**
 * Google Cloud resource detector which populates attributes based on the environment this
 * process is running in. If not on GCP, returns an empty resource.
 */
class GcpDetectorSync {
    async _asyncAttributes() {
        return (await detect()).attributes;
    }
    detect() {
        const asyncAttributes = this._asyncAttributes();
        const attributes = {};
        ATTRIBUTE_NAMES.forEach(name => {
            // Each resource attribute is determined asynchronously in _gatherData().
            attributes[name] = asyncAttributes.then(data => data[name]);
        });
        return { attributes };
    }
}
exports.GcpDetectorSync = GcpDetectorSync;
//# sourceMappingURL=detector.js.map