"use strict";
// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
Object.defineProperty(exports, "__esModule", { value: true });
exports.GcpDetectorSync = exports.mapOtelResourceToMonitoredResource = void 0;
const semantic_conventions_1 = require("@opentelemetry/semantic-conventions");
const AWS_ACCOUNT = 'aws_account';
const AWS_EC2_INSTANCE = 'aws_ec2_instance';
const CLOUD_FUNCTION = 'cloud_function';
const CLOUD_RUN_REVISION = 'cloud_run_revision';
const CLUSTER_NAME = 'cluster_name';
const CONFIGURATION_NAME = 'configuration_name';
const CONTAINER_NAME = 'container_name';
const FUNCTION_NAME = 'function_name';
const GAE_INSTANCE = 'gae_instance';
const GAE_MODULE_ID = 'module_id';
const GAE_VERSION_ID = 'version_id';
const GCE_INSTANCE = 'gce_instance';
const GENERIC_NODE = 'generic_node';
const GENERIC_TASK = 'generic_task';
const INSTANCE_ID = 'instance_id';
const JOB = 'job';
const K8S_CLUSTER = 'k8s_cluster';
const K8S_CONTAINER = 'k8s_container';
const K8S_NODE = 'k8s_node';
const K8S_POD = 'k8s_pod';
const LOCATION = 'location';
const NAMESPACE = 'namespace';
const NAMESPACE_NAME = 'namespace_name';
const NODE_ID = 'node_id';
const NODE_NAME = 'node_name';
const POD_NAME = 'pod_name';
const REGION = 'region';
const REVISION_NAME = 'revision_name';
const SERVICE_NAME = 'service_name';
const TASK_ID = 'task_id';
const ZONE = 'zone';
const UNKNOWN_SERVICE_PREFIX = 'unknown_service';
/**
 * Mappings of GCM resource label keys onto mapping config from OTel resource for a given
 * monitored resource type. Copied from Go impl:
 * https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/v1.8.0/internal/resourcemapping/resourcemapping.go#L51
 */
const MAPPINGS = {
    [GCE_INSTANCE]: {
        [ZONE]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE],
        },
        [INSTANCE_ID]: { otelKeys: [semantic_conventions_1.SEMRESATTRS_HOST_ID] },
    },
    [K8S_CONTAINER]: {
        [LOCATION]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE, semantic_conventions_1.SEMRESATTRS_CLOUD_REGION],
        },
        [CLUSTER_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_CLUSTER_NAME],
        },
        [NAMESPACE_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_NAMESPACE_NAME],
        },
        [POD_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_POD_NAME],
        },
        [CONTAINER_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_CONTAINER_NAME],
        },
    },
    [K8S_POD]: {
        [LOCATION]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE, semantic_conventions_1.SEMRESATTRS_CLOUD_REGION],
        },
        [CLUSTER_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_CLUSTER_NAME],
        },
        [NAMESPACE_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_NAMESPACE_NAME],
        },
        [POD_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_POD_NAME],
        },
    },
    [K8S_NODE]: {
        [LOCATION]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE, semantic_conventions_1.SEMRESATTRS_CLOUD_REGION],
        },
        [CLUSTER_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_CLUSTER_NAME],
        },
        [NODE_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_NODE_NAME],
        },
    },
    [K8S_CLUSTER]: {
        [LOCATION]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE, semantic_conventions_1.SEMRESATTRS_CLOUD_REGION],
        },
        [CLUSTER_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_K8S_CLUSTER_NAME],
        },
    },
    [AWS_EC2_INSTANCE]: {
        [INSTANCE_ID]: { otelKeys: [semantic_conventions_1.SEMRESATTRS_HOST_ID] },
        [REGION]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE, semantic_conventions_1.SEMRESATTRS_CLOUD_REGION],
        },
        [AWS_ACCOUNT]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_ACCOUNT_ID],
        },
    },
    [CLOUD_RUN_REVISION]: {
        [LOCATION]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_REGION],
        },
        [SERVICE_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_FAAS_NAME],
        },
        [CONFIGURATION_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_FAAS_NAME],
        },
        [REVISION_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_FAAS_VERSION],
        },
    },
    [CLOUD_FUNCTION]: {
        [REGION]: { otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_REGION] },
        [FUNCTION_NAME]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_FAAS_NAME],
        },
    },
    [GAE_INSTANCE]: {
        [LOCATION]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE, semantic_conventions_1.SEMRESATTRS_CLOUD_REGION],
        },
        [GAE_MODULE_ID]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_FAAS_NAME],
        },
        [GAE_VERSION_ID]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_FAAS_VERSION],
        },
        [INSTANCE_ID]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_FAAS_INSTANCE],
        },
    },
    [GENERIC_TASK]: {
        [LOCATION]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE, semantic_conventions_1.SEMRESATTRS_CLOUD_REGION],
            fallback: 'global',
        },
        [NAMESPACE]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_SERVICE_NAMESPACE],
        },
        [JOB]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_SERVICE_NAME, semantic_conventions_1.SEMRESATTRS_FAAS_NAME],
        },
        [TASK_ID]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_SERVICE_INSTANCE_ID, semantic_conventions_1.SEMRESATTRS_FAAS_INSTANCE],
        },
    },
    [GENERIC_NODE]: {
        [LOCATION]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_CLOUD_AVAILABILITY_ZONE, semantic_conventions_1.SEMRESATTRS_CLOUD_REGION],
            fallback: 'global',
        },
        [NAMESPACE]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_SERVICE_NAMESPACE],
        },
        [NODE_ID]: {
            otelKeys: [semantic_conventions_1.SEMRESATTRS_HOST_ID, semantic_conventions_1.SEMRESATTRS_HOST_NAME],
        },
    },
};
/**
 * Given an OTel resource, return a MonitoredResource. Copied from the collector's
 * implementation in Go:
 * https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/v1.8.0/internal/resourcemapping/resourcemapping.go#L51
 *
 * @param resource the OTel Resource
 * @returns the corresponding GCM MonitoredResource
 */
function mapOtelResourceToMonitoredResource(resource) {
    const attrs = resource.attributes;
    const platform = attrs[semantic_conventions_1.SEMRESATTRS_CLOUD_PLATFORM];
    let mr;
    if (platform === semantic_conventions_1.CLOUDPLATFORMVALUES_GCP_COMPUTE_ENGINE) {
        mr = createMonitoredResource(GCE_INSTANCE, attrs);
    }
    else if (platform === semantic_conventions_1.CLOUDPLATFORMVALUES_GCP_APP_ENGINE) {
        mr = createMonitoredResource(GAE_INSTANCE, attrs);
    }
    else if (platform === semantic_conventions_1.CLOUDPLATFORMVALUES_AWS_EC2) {
        mr = createMonitoredResource(AWS_EC2_INSTANCE, attrs);
    }
    else if (semantic_conventions_1.SEMRESATTRS_K8S_CLUSTER_NAME in attrs) {
        // if k8s.cluster.name is set, pattern match for various k8s resources.
        // this will also match non-cloud k8s platforms like minikube.
        if (semantic_conventions_1.SEMRESATTRS_K8S_CONTAINER_NAME in attrs) {
            mr = createMonitoredResource(K8S_CONTAINER, attrs);
        }
        else if (semantic_conventions_1.SEMRESATTRS_K8S_POD_NAME in attrs) {
            mr = createMonitoredResource(K8S_POD, attrs);
        }
        else if (semantic_conventions_1.SEMRESATTRS_K8S_NODE_NAME in attrs) {
            mr = createMonitoredResource(K8S_NODE, attrs);
        }
        else {
            mr = createMonitoredResource(K8S_CLUSTER, attrs);
        }
    }
    else if ((semantic_conventions_1.SEMRESATTRS_SERVICE_NAME in attrs || semantic_conventions_1.SEMRESATTRS_FAAS_NAME in attrs) &&
        (semantic_conventions_1.SEMRESATTRS_SERVICE_INSTANCE_ID in attrs ||
            semantic_conventions_1.SEMRESATTRS_FAAS_INSTANCE in attrs)) {
        // fallback to generic_task
        mr = createMonitoredResource(GENERIC_TASK, attrs);
    }
    else {
        // If not possible, finally fallback to generic_node
        mr = createMonitoredResource(GENERIC_NODE, attrs);
    }
    return mr;
}
exports.mapOtelResourceToMonitoredResource = mapOtelResourceToMonitoredResource;
function createMonitoredResource(monitoredResourceType, resourceAttrs) {
    const mapping = MAPPINGS[monitoredResourceType];
    const labels = {};
    Object.entries(mapping).map(([mrKey, mapConfig]) => {
        let mrValue;
        for (const otelKey of mapConfig.otelKeys) {
            if (otelKey in resourceAttrs &&
                !resourceAttrs[otelKey]?.toString()?.startsWith(UNKNOWN_SERVICE_PREFIX)) {
                mrValue = resourceAttrs[otelKey];
                break;
            }
        }
        if (mrValue === undefined &&
            mapConfig.otelKeys.includes(semantic_conventions_1.SEMRESATTRS_SERVICE_NAME)) {
            // The service name started with unknown_service, was ignored above, and we couldn't find
            // a better value for mrValue.
            mrValue = resourceAttrs[semantic_conventions_1.SEMRESATTRS_SERVICE_NAME];
        }
        if (mrValue === undefined) {
            mrValue = mapConfig.fallback ?? '';
        }
        // OTel attribute values can be any of string, boolean, number, or array of any of them.
        // Encode any non-strings as json string
        if (typeof mrValue !== 'string') {
            mrValue = JSON.stringify(mrValue);
        }
        labels[mrKey] = mrValue;
    });
    return {
        type: monitoredResourceType,
        labels,
    };
}
var detector_1 = require("./detector/detector");
Object.defineProperty(exports, "GcpDetectorSync", { enumerable: true, get: function () { return detector_1.GcpDetectorSync; } });
//# sourceMappingURL=index.js.map