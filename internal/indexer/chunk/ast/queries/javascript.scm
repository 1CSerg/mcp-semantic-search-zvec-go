(function_declaration
  name: (identifier) @name) @boundary.function

(class_declaration
  name: (identifier) @name) @boundary.class

(method_definition
  name: (property_identifier) @name) @boundary.method

(export_statement) @boundary.export

(lexical_declaration
  (variable_declarator
    name: (identifier) @name)) @boundary.declaration

(variable_declaration
  (variable_declarator
    name: (identifier) @name)) @boundary.declaration
