"""Read unambiguous external chart metadata and Helm release indices."""

import re
from typing import Any

import yaml


class StrictLoader(yaml.SafeLoader):
    pass


# Preserve YAML 1.2 boolean spelling in external chart metadata and release indices.
StrictLoader.yaml_implicit_resolvers = {
    key: [(tag, pattern) for tag, pattern in values if tag != "tag:yaml.org,2002:bool"]
    for key, values in yaml.SafeLoader.yaml_implicit_resolvers.items()
}
StrictLoader.add_implicit_resolver(
    "tag:yaml.org,2002:bool", re.compile(r"^(true|false)$", re.IGNORECASE), list("tTfF")
)


def mapping(loader: StrictLoader, node: yaml.MappingNode) -> dict[Any, Any]:
    result: dict[Any, Any] = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node)
        if not isinstance(key, (str, int, float, bool, type(None))) or key in result:
            raise ValueError("Duplicate or unsupported YAML mapping key")

        result[key] = loader.construct_object(value_node)

    return result


StrictLoader.add_constructor("tag:yaml.org,2002:map", mapping)


def load_yaml(text: str) -> dict[str, Any]:
    try:
        for event in yaml.parse(text):
            if isinstance(event, yaml.AliasEvent) or getattr(event, "anchor", None):
                raise ValueError("YAML aliases and anchors are not supported")

            if getattr(event, "tag", None):
                raise ValueError("Explicit YAML tags are not supported")

        value = yaml.load(text, Loader=StrictLoader)
    except yaml.YAMLError as error:
        raise ValueError("Invalid YAML syntax") from error

    if not isinstance(value, dict):
        raise ValueError("Expected one YAML mapping")

    return value
