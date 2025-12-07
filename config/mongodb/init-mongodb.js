// MongoDB initialization script for ElasticRelay
// This script creates the necessary user and sample collections

// Switch to admin database for user creation
db = db.getSiblingDB('admin');

// Create ElasticRelay user with readWrite and changeStream permissions
db.createUser({
  user: 'elasticrelay_user',
  pwd: 'elasticrelay_pass',
  roles: [
    { role: 'readWrite', db: 'elasticrelay' },
    { role: 'read', db: 'local' }  // Required for Change Streams
  ]
});

print('Created elasticrelay_user');

// Switch to elasticrelay database
db = db.getSiblingDB('elasticrelay');

// Create sample collections with some test data

// Users collection
db.createCollection('users');
db.users.insertMany([
  {
    _id: ObjectId(),
    name: 'Alice Johnson',
    email: 'alice@example.com',
    age: 28,
    address: {
      street: '123 Main St',
      city: 'New York',
      country: 'USA'
    },
    tags: ['developer', 'nodejs'],
    created_at: new Date(),
    updated_at: new Date()
  },
  {
    _id: ObjectId(),
    name: 'Bob Smith',
    email: 'bob@example.com',
    age: 35,
    address: {
      street: '456 Oak Ave',
      city: 'San Francisco',
      country: 'USA'
    },
    tags: ['manager', 'product'],
    created_at: new Date(),
    updated_at: new Date()
  }
]);
print('Created users collection with sample data');

// Orders collection
db.createCollection('orders');
db.orders.insertMany([
  {
    _id: ObjectId(),
    order_id: 'ORD-001',
    user_email: 'alice@example.com',
    items: [
      { product: 'Laptop', quantity: 1, price: 999.99 },
      { product: 'Mouse', quantity: 2, price: 29.99 }
    ],
    total: 1059.97,
    status: 'completed',
    created_at: new Date()
  },
  {
    _id: ObjectId(),
    order_id: 'ORD-002',
    user_email: 'bob@example.com',
    items: [
      { product: 'Keyboard', quantity: 1, price: 149.99 }
    ],
    total: 149.99,
    status: 'pending',
    created_at: new Date()
  }
]);
print('Created orders collection with sample data');

// Products collection
db.createCollection('products');
db.products.insertMany([
  {
    _id: ObjectId(),
    sku: 'LAPTOP-001',
    name: 'Professional Laptop',
    description: 'High-performance laptop for developers',
    price: 999.99,
    category: 'electronics',
    inventory: 50,
    attributes: {
      brand: 'TechCo',
      screen_size: '15.6"',
      memory: '16GB'
    },
    created_at: new Date()
  },
  {
    _id: ObjectId(),
    sku: 'MOUSE-001',
    name: 'Wireless Mouse',
    description: 'Ergonomic wireless mouse',
    price: 29.99,
    category: 'accessories',
    inventory: 200,
    attributes: {
      brand: 'PeriphCo',
      connectivity: 'Bluetooth'
    },
    created_at: new Date()
  }
]);
print('Created products collection with sample data');

// Create indexes for better performance
db.users.createIndex({ email: 1 }, { unique: true });
db.orders.createIndex({ order_id: 1 }, { unique: true });
db.orders.createIndex({ user_email: 1 });
db.products.createIndex({ sku: 1 }, { unique: true });
db.products.createIndex({ category: 1 });

print('Created indexes');
print('MongoDB initialization completed successfully!');
