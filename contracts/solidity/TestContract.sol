// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title TestContract
 * @dev Simple contract for testing EVM deployment on Guru network
 */
contract TestContract {
    uint256 public value;
    string public message;
    address public owner;
    
    event ValueChanged(uint256 oldValue, uint256 newValue);
    event MessageChanged(string oldMessage, string newMessage);
    
    constructor(uint256 _initialValue, string memory _initialMessage) {
        value = _initialValue;
        message = _initialMessage;
        owner = msg.sender;
    }
    
    function setValue(uint256 _newValue) public {
        uint256 oldValue = value;
        value = _newValue;
        emit ValueChanged(oldValue, _newValue);
    }
    
    function setMessage(string memory _newMessage) public {
        string memory oldMessage = message;
        message = _newMessage;
        emit MessageChanged(oldMessage, _newMessage);
    }
    
    function getValue() public view returns (uint256) {
        return value;
    }
    
    function getMessage() public view returns (string memory) {
        return message;
    }
    
    function getOwner() public view returns (address) {
        return owner;
    }
    
    // Function to test gas estimation
    function complexOperation() public pure returns (uint256) {
        uint256 result = 0;
        for (uint256 i = 0; i < 100; i++) {
            result += i * i;
        }
        return result;
    }
}
