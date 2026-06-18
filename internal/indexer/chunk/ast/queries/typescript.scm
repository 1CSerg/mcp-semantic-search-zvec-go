(function_declaration
  name: (identifier) @name) @boundary.function

(class_declaration
  name: (type_identifier) @name) @boundary.class

(method_definition
  name: (property_identifier) @name) @boundary.method

(interface_declaration
  name: (type_identifier) @name) @boundary.interface

(type_alias_declaration
  name: (type_identifier) @name) @boundary.type_alias

(enum_declaration
  name: (identifier) @name) @boundary.enum

(export_statement) @boundary.export

(internal_module
  name: (_) @name) @boundary.namespace

(lexical_declaration
  (variable_declarator
    name: (identifier) @name)) @boundary.declaration

(variable_declaration
  (variable_declarator
    name: (identifier) @name)) @boundary.declaration
